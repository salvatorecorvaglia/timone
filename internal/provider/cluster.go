package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"

	"timone/internal/config"
	"timone/internal/integrations"
	"timone/internal/model"
	"timone/web"
)

const collectionLimit = 5000
const readLease = 10 * time.Second

var eventKind = Kind{Name: "Event", Version: "v1", Plural: "events", Namespaced: true}

type cachedList struct {
	items     []model.Object
	truncated bool
	at        time.Time
	bytes     int
}
type mappingEntry struct {
	spec Kind
	err  error
}
type ClusterIdentityProvider interface{ ClusterIdentity() string }

func (k *cluster) ClusterIdentity() string { return k.identity }

type cluster struct {
	identity                      string
	desiredTransport              http.RoundTripper // injected only by in-process transport tests
	desired                       integrations.DesiredOptions
	name, namespace, server, auth string
	client                        *http.Client
	dynamic                       dynamic.Interface
	mapper                        *restmapper.DeferredDiscoveryRESTMapper
	mu                            sync.Mutex
	cache                         map[string]cachedList
	flights                       map[string]chan struct{}
	streams                       map[string]*resourceStream
	watchCtx                      context.Context
	notify                        func()
	notifyScoped                  func(model.ResourceChange)
	workers                       sync.WaitGroup
	lastRead                      time.Time
	failures                      map[string]string
	mapMu                         sync.Mutex
	mappings                      map[string]mappingEntry
	mappedAt                      time.Time
	helmDetails                   bool
	customResources               []string
	discoveryNote                 string
	retentionBudget               int
	retentionWarnings             map[string]string
	readSlots                     chan struct{}
}

func NewCluster(c config.Config) (Provider, error) {
	cfg, name, ns, auth, e := loadKubernetesConfig(c)
	if e != nil {
		return nil, e
	}
	client, e := rest.HTTPClientFor(cfg)
	if e != nil {
		return nil, fmt.Errorf("cannot initialize Kubernetes TLS/authentication transport")
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// Limit bodies before client-go decodes them, including discovery and list responses.
	client.Transport = limitedTransport{next: client.Transport}
	dc, e := dynamic.NewForConfigAndClient(cfg, client)
	if e != nil {
		return nil, fmt.Errorf("cannot initialize Kubernetes dynamic client")
	}
	discoveryClient, e := discovery.NewDiscoveryClientForConfigAndClient(cfg, client)
	if e != nil {
		return nil, fmt.Errorf("cannot initialize Kubernetes discovery client")
	}
	k := newCluster(name, ns, client, dc)
	k.server = strings.TrimRight(cfg.Host, "/")
	k.identity = fmt.Sprintf("%x", sha256.Sum256([]byte(name+"\x00"+k.server+"\x00"+cfg.ServerName+"\x00"+cfg.CAFile+"\x00"+string(cfg.CAData))))
	if c.RetainedMiB > 0 {
		k.retentionBudget = c.RetainedMiB << 20
	}
	k.auth = auth
	k.desired = c.GitOps
	k.helmDetails = c.HelmDetails
	k.customResources = append([]string{}, c.CustomResources...)
	k.mapper = restmapper.NewDeferredDiscoveryRESTMapperWithContext(memory.NewMemCacheClientWithContext(discoveryClient))
	return k, nil
}
func newCluster(name, ns string, client *http.Client, dc dynamic.Interface) *cluster {
	return &cluster{retentionBudget: 16 << 20, retentionWarnings: map[string]string{}, readSlots: make(chan struct{}, 2), name: name, namespace: ns, client: client, dynamic: dc, cache: map[string]cachedList{}, flights: map[string]chan struct{}{}, streams: map[string]*resourceStream{}, failures: map[string]string{}, mappings: map[string]mappingEntry{}}
}
func loadKubernetesConfig(c config.Config) (*rest.Config, string, string, string, error) {
	var cfg *rest.Config
	var e error
	name, ns, auth := "in-cluster", c.Namespace, "service-account"
	if c.Mode == "incluster" {
		cfg, e = rest.InClusterConfig()
		if e != nil {
			return nil, "", "", "", fmt.Errorf("cannot load in-cluster ServiceAccount credentials")
		}
	} else {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		if c.Kubeconfig != "" {
			rules.Precedence = filepath.SplitList(c.Kubeconfig)
			rules.ExplicitPath = ""
		}
		loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: c.Context})
		raw, err := loader.RawConfig()
		if err != nil {
			return nil, "", "", "", fmt.Errorf("cannot load kubeconfig files; check paths and YAML syntax")
		}
		name = c.Context
		if name == "" {
			name = raw.CurrentContext
		}
		selected, exists := raw.Contexts[name]
		if !exists {
			if len(raw.Contexts) == 0 {
				return nil, "", "", "", fmt.Errorf("no contexts found in kubeconfig; configure access to a Kubernetes cluster, or run timone --mode demo")
			}
			if name == "" {
				return nil, "", "", "", fmt.Errorf("no current kubeconfig context selected; list contexts with kubectl config get-contexts and select one with timone --context <name>")
			}
			return nil, "", "", "", fmt.Errorf("selected kubeconfig context does not exist; list contexts with kubectl config get-contexts and select one with timone --context <name>")
		}
		if ns == "" && !c.NamespaceExplicit {
			ns = selected.Namespace
		}
		cfg, e = loader.ClientConfig()
		if e != nil {
			return nil, "", "", "", fmt.Errorf("cannot initialize selected kubeconfig context; check cluster and user configuration")
		}
		auth = "configured-credentials"
		if cfg.BearerToken != "" || cfg.BearerTokenFile != "" {
			auth = "token"
		}
		if cfg.CertFile != "" || len(cfg.CertData) > 0 {
			auth = "client-certificate"
		}
	}
	if !validNamespace(ns) {
		return nil, "", "", "", fmt.Errorf("configured namespace is invalid")
	}
	u, e := url.Parse(cfg.Host)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, "", "", "", fmt.Errorf("Kubernetes server must be an HTTPS URL")
	}
	if cfg.Insecure {
		return nil, "", "", "", fmt.Errorf("insecure-skip-tls-verify is not supported; configure a trusted CA")
	}
	if cfg.ExecProvider != nil {
		if !c.AllowExecAuth {
			return nil, "", "", "", fmt.Errorf("exec authentication is disabled; explicitly set TIMONE_ALLOW_EXEC_AUTH=true for a trusted local kubeconfig")
		}
		if cfg.ExecProvider.InteractiveMode == clientcmdapi.AlwaysExecInteractiveMode {
			return nil, "", "", "", fmt.Errorf("Timone requires a non-interactive exec credential plugin")
		}
		executable, err := os.Executable()
		if err != nil {
			return nil, "", "", "", fmt.Errorf("cannot locate Timone credential helper")
		}
		original := cfg.ExecProvider.Command
		args := append([]string{"credential-exec", original}, cfg.ExecProvider.Args...)
		cfg.ExecProvider = cfg.ExecProvider.DeepCopy()
		cfg.ExecProvider.Command = executable
		cfg.ExecProvider.Args = args
		cfg.ExecProvider.InteractiveMode = clientcmdapi.NeverExecInteractiveMode
		cfg.ExecProvider.StdinUnavailable = true
		cfg.ExecProvider.InstallHint = ""
		auth = "exec-plugin"
	}
	// Legacy auth-provider plugins are not supported by Kubernetes upstream cloud plugins.
	if cfg.AuthProvider != nil {
		return nil, "", "", "", fmt.Errorf("legacy auth-provider configuration is unsupported; migrate to an exec credential plugin")
	}
	cfg.Timeout = 30 * time.Second
	cfg.QPS = 20
	cfg.Burst = 30
	cfg.UserAgent = "timone/" + web.Version
	cfg.WarningHandler = rest.NoWarnings{}
	return cfg, name, ns, auth, nil
}
func validNamespace(ns string) bool {
	if ns == "" {
		return true
	}
	if len(ns) > 63 {
		return false
	}
	for i, c := range ns {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			continue
		}
		if c == '-' && i > 0 && i < len(ns)-1 {
			continue
		}
		return false
	}
	return true
}
func (k *cluster) Name() string                 { return k.name }
func (k *cluster) Mode() string                 { return "cluster" }
func (k *cluster) Namespace() string            { return k.namespace }
func quiet(ctx context.Context) context.Context { return klog.NewContext(ctx, logr.Discard()) }
func safeError(err error) error {
	if err == nil {
		return nil
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	code := 503
	message := "Kubernetes API connection failed; check endpoint, certificates, credentials and network access"
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		code = 504
		message = "Kubernetes API request timed out"
	case apierrors.IsUnauthorized(err):
		code = 401
		message = "Kubernetes authentication failed"
	case apierrors.IsForbidden(err):
		code = 403
		message = "Kubernetes RBAC denied this read"
	case apierrors.IsNotFound(err) || meta.IsNoMatchError(err):
		code = 404
		message = "Resource or API is unavailable"
	case apierrors.IsTooManyRequests(err):
		code = 429
		message = "Kubernetes API rate limit exceeded"
	case apierrors.IsResourceExpired(err):
		code = 410
		message = "Kubernetes resource version expired; resynchronizing"
	}
	return &Error{Code: code, Message: message}
}
func (k *cluster) scope(spec Kind, ns string) (string, error) {
	if !spec.Namespaced {
		return "", nil
	}
	if !validNamespace(ns) {
		return "", &Error{Code: 400, Message: "Invalid namespace"}
	}
	if k.namespace != "" {
		if ns != "" && ns != k.namespace {
			return "", &Error{Code: 403, Message: "Namespace is outside configured scope"}
		}
		ns = k.namespace
	}
	return ns, nil
}
func (k *cluster) mapping(ctx context.Context, spec Kind) (Kind, error) {
	if k.mapper == nil {
		return spec, nil
	}
	k.mapMu.Lock()
	defer k.mapMu.Unlock()
	if time.Since(k.mappedAt) > time.Minute {
		k.mapper.ResetWithContext(quiet(ctx))
		k.mappings = map[string]mappingEntry{}
		k.discoveryNote = ""
		k.mappedAt = time.Now()
	}
	if e, ok := k.mappings[spec.Name]; ok {
		return e.spec, e.err
	}
	m, e := k.mapper.RESTMappingWithContext(quiet(ctx), schema.GroupKind{Group: spec.Group, Kind: spec.Name})
	if e != nil {
		if meta.IsNoMatchError(e) {
			err := safeError(e)
			k.mappings[spec.Name] = mappingEntry{spec, err}
			return spec, err
		}
		k.discoveryNote = "API discovery unavailable; using the allowlisted API version until discovery recovers"
		k.mappings[spec.Name] = mappingEntry{spec, nil}
		return spec, nil
	}
	if m.Resource.Group != spec.Group || m.Resource.Resource != spec.Plural || (m.Scope.Name() == meta.RESTScopeNameNamespace) != spec.Namespaced {
		return spec, &Error{Code: 502, Message: "Discovered API mapping does not match the supported resource"}
	}
	spec.Version = m.Resource.Version
	k.mappings[spec.Name] = mappingEntry{spec, nil}
	return spec, nil
}
func (k *cluster) resource(spec Kind, ns string) dynamic.ResourceInterface {
	r := k.dynamic.Resource(schema.GroupVersionResource{Group: spec.Group, Version: spec.Version, Resource: spec.Plural})
	if spec.Namespaced {
		return r.Namespace(ns)
	}
	return r
}
func (k *cluster) observe(key string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err != nil {
		if len(k.failures) >= 256 {
			k.failures = map[string]string{}
		}
		k.failures[key] = safeError(err).Error()
		delete(k.cache, key)
	} else {
		delete(k.failures, key)
		k.lastRead = time.Now().UTC()
	}
}
func listKey(spec Kind, ns string) string { return spec.Name + ":" + ns }
func (k *cluster) directList(ctx context.Context, spec Kind, ns string, options metav1.ListOptions) (*unstructured.UnstructuredList, bool, error) {
	return k.directListProjected(ctx, spec, ns, options, model.Sanitize)
}
func (k *cluster) directListProjected(ctx context.Context, spec Kind, ns string, options metav1.ListOptions, project func(model.Object) model.Object) (*unstructured.UnstructuredList, bool, error) {
	select {
	case k.readSlots <- struct{}{}:
		defer func() { <-k.readSlots }()
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	result := &unstructured.UnstructuredList{}
	options.Limit = 500
	options.Continue = ""
	seen := map[string]bool{}
	bytes := 0
	for {
		page, e := k.resource(spec, ns).List(quiet(ctx), options)
		if e != nil {
			return nil, false, e
		}
		if result.GetResourceVersion() == "" {
			result.SetResourceVersion(page.GetResourceVersion())
		}
		for i, item := range page.Items {
			item.SetKind(spec.Name)
			if item.GetAPIVersion() == "" {
				item.SetAPIVersion(schema.GroupVersion{Group: spec.Group, Version: spec.Version}.String())
			}
			o := project(model.Object(item.Object))
			encoded, _ := json.Marshal(o)
			if err := consumeReadBudget(ctx, len(encoded)); err != nil {
				return nil, false, err
			}
			bytes += len(encoded)
			if bytes > 16<<20 {
				return nil, false, &Error{Code: 413, Message: "Resource collection exceeds 16 MiB; narrow namespace scope"}
			}
			result.Items = append(result.Items, unstructured.Unstructured{Object: map[string]any(o)})
			if len(result.Items) == collectionLimit {
				return result, page.GetContinue() != "" || i+1 < len(page.Items), nil
			}
		}
		cont := page.GetContinue()
		if cont == "" {
			return result, false, nil
		}
		if seen[cont] {
			return nil, false, &Error{Code: 502, Message: "Kubernetes API repeated a list continuation token"}
		}
		seen[cont] = true
		options.Continue = cont
	}
}
func objects(list *unstructured.UnstructuredList) []model.Object {
	out := make([]model.Object, 0, len(list.Items))
	for _, o := range list.Items {
		out = append(out, model.CloneProjected(model.Object(o.Object)))
	}
	sortObjects(out)
	return out
}
func sortObjects(o []model.Object) {
	sort.Slice(o, func(i, j int) bool {
		return model.Str(o[i], "metadata", "namespace")+"/"+model.Str(o[i], "metadata", "name") < model.Str(o[j], "metadata", "namespace")+"/"+model.Str(o[j], "metadata", "name")
	})
}
func (k *cluster) list(ctx context.Context, spec Kind, ns string) ([]model.Object, bool, error) {
	if e := ctx.Err(); e != nil {
		return nil, false, e
	}
	ns, e := k.scope(spec, ns)
	if e != nil {
		return nil, false, e
	}
	spec, e = k.mapping(ctx, spec)
	if e != nil {
		return nil, false, e
	}
	key := listKey(spec, ns)
	for {
		k.mu.Lock()
		k.expireRetentionLocked(time.Now())
		entry, ok := k.cache[key]
		stream := k.streams[key]
		if stream != nil {
			stream.lastUsed = time.Now()
		}
		if ok && time.Since(entry.at) < readLease {
			if stream != nil && stream.informer != nil && stream.healthy && stream.informer.HasSynced() {
				informer := stream.informer
				if err := consumeReadBudget(ctx, stream.retainedBytes); err != nil {
					k.mu.Unlock()
					return nil, false, err
				}
				k.mu.Unlock()
				out := streamObjects(informer)
				k.mu.Lock()
				valid := k.streams[key] == stream && stream.healthy
				k.mu.Unlock()
				if valid && len(out) <= collectionLimit {
					return out, false, nil
				}
				if !valid {
					continue
				}
				k.mu.Lock()
				delete(k.cache, key)
				stream.healthy = false
				k.mu.Unlock()
				continue
			}
			if err := consumeReadBudget(ctx, entry.bytes); err != nil {
				k.mu.Unlock()
				return nil, false, err
			}
			k.mu.Unlock()
			return cloneObjects(entry.items), entry.truncated, nil
		}
		if wait, ok := k.flights[key]; ok {
			k.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-wait:
				continue
			}
		}
		k.flights[key] = make(chan struct{})
		k.mu.Unlock()
		break
	}
	list, truncated, e := k.directList(ctx, spec, ns, metav1.ListOptions{})
	// Projection owns its copies; only publication and accounting need the lock.
	var items []model.Object
	var data []byte
	if e == nil {
		items = objects(list)
		data, _ = json.Marshal(items)
	}
	k.mu.Lock()
	wait := k.flights[key]
	delete(k.flights, key)
	if e == nil {
		delete(k.cache, key)
		if k.reserveCacheLocked(len(data)) {
			k.cache[key] = cachedList{items: items, truncated: truncated, at: time.Now(), bytes: len(data)}
		}

		delete(k.failures, key)
		k.lastRead = time.Now().UTC()
	} else {
		delete(k.cache, key)
		if len(k.failures) >= 256 {
			k.failures = map[string]string{}
		}
		k.failures[key] = safeError(e).Error()
	}
	if wait != nil {
		close(wait)
	}
	k.mu.Unlock()
	if e != nil {
		return nil, false, safeError(e)
	}
	if !truncated {
		k.ensureStream(spec, ns, list)
	}
	return cloneObjects(items), truncated, nil
}
func cloneObjects(items []model.Object) []model.Object {
	out := make([]model.Object, 0, len(items))
	for _, o := range items {
		out = append(out, model.CloneProjected(o))
	}
	return out
}
func (k *cluster) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	spec, ok := FindKind(kind)
	if !ok {
		return nil, false, &Error{Code: 400, Message: "Unsupported resource kind"}
	}
	return k.list(ctx, spec, ns)
}
func (k *cluster) Get(ctx context.Context, kind, ns, name string) (model.Object, error) {
	spec, ok := FindKind(kind)
	if !ok {
		return nil, &Error{Code: 400, Message: "Unsupported resource kind"}
	}
	var e error
	ns, e = k.scope(spec, ns)
	if e != nil {
		return nil, e
	}
	if spec.Namespaced && ns == "" {
		return nil, &Error{Code: 400, Message: "Namespace is required"}
	}
	spec, e = k.mapping(ctx, spec)
	if e != nil {
		return nil, e
	}
	o, e := k.resource(spec, ns).Get(quiet(ctx), name, metav1.GetOptions{})
	k.observe(listKey(spec, ns), e)
	if e != nil {
		return nil, safeError(e)
	}
	o.SetKind(kind)
	return model.Sanitize(model.Object(o.Object)), nil
}
func (k *cluster) Events(ctx context.Context, ns string) ([]model.Event, error) {
	items, truncated, e := k.list(ctx, eventKind, ns)
	if e != nil {
		return nil, e
	}
	if truncated {
		return nil, &Error{Code: 413, Message: "Event collection exceeds 5000 entries; narrow namespace scope"}
	}
	out := []model.Event{}
	for _, o := range items {
		ts := model.Str(o, "series", "lastObservedTime")
		if ts == "" {
			ts = model.Str(o, "lastTimestamp")
		}
		if ts == "" {
			ts = model.Str(o, "eventTime")
		}
		if ts == "" {
			ts = model.Str(o, "metadata", "creationTimestamp")
		}
		first := model.Str(o, "firstTimestamp")
		if first == "" {
			first = model.Str(o, "eventTime")
		}
		if first == "" {
			first = model.Str(o, "metadata", "creationTimestamp")
		}
		out = append(out, model.Event{ID: model.Str(o, "metadata", "uid"), Namespace: model.Str(o, "metadata", "namespace"), Resource: model.Str(o, "involvedObject", "name"), Kind: model.Str(o, "involvedObject", "kind"), ResourceID: model.ID(model.Str(o, "involvedObject", "kind"), model.Str(o, "involvedObject", "namespace"), model.Str(o, "involvedObject", "name")), ResourceNamespace: model.Str(o, "involvedObject", "namespace"), ResourceUID: model.Str(o, "involvedObject", "uid"), Type: model.Str(o, "type"), Reason: model.Str(o, "reason"), Message: model.Str(o, "message"), Count: max(1, int(model.Num(o, "count")), int(model.Num(o, "series", "count"))), Timestamp: ts, FirstObserved: first})
	}
	return out, nil
}
func (k *cluster) request(ctx context.Context, path string) (*http.Response, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, k.server+path, nil)
	if e != nil {
		return nil, safeError(e)
	}
	req.Header.Set("Accept", "application/json")
	res, e := k.client.Do(req)
	if e != nil {
		return nil, safeError(e)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		message := "Kubernetes API request failed"
		switch res.StatusCode {
		case 401:
			message = "Kubernetes authentication failed"
		case 403:
			message = "Kubernetes RBAC denied this read"
		case 404:
			message = "Resource or API is unavailable"
		case 429:
			message = "Kubernetes API rate limit exceeded"
		}
		return nil, &Error{Code: res.StatusCode, Message: message}
	}
	return res, nil
}
func (k *cluster) read(ctx context.Context, path string, v any) error {
	res, e := k.request(ctx, path)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, (16<<20)+1))
	if e != nil {
		return safeError(e)
	}
	if len(b) > 16<<20 {
		return &Error{Code: 413, Message: "Kubernetes response exceeds 16 MiB; narrow namespace scope"}
	}
	if err := consumeReadBudget(ctx, len(b)); err != nil {
		return err
	}
	if json.Unmarshal(b, v) != nil {
		return &Error{Code: 502, Message: "Invalid response from Kubernetes API"}
	}
	return nil
}

type limitedTransport struct{ next http.RoundTripper }

func (t limitedTransport) WrappedRoundTripper() http.RoundTripper { return t.next }
func (t limitedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	res, e := next.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	res.Body = &limitedBody{ReadCloser: res.Body, remaining: 16 << 20}
	return res, nil
}

type limitedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		var probe [1]byte
		n, e := b.ReadCloser.Read(probe[:])
		if n == 0 {
			return 0, e
		}
		return 0, &Error{Code: 413, Message: "Kubernetes response exceeds 16 MiB; narrow namespace scope"}
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, e := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, e
}
