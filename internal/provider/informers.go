package provider

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"timone/internal/model"
)

type resourceStream struct {
	informer        cache.SharedIndexInformer
	cancel          context.CancelFunc
	healthy         bool // fields below are protected by cluster.mu
	state           string
	message         string
	retryAfter      time.Time
	lastSync        time.Time
	kind, namespace string
	objectCount     int
	lastUsed        time.Time
	retainedBytes   int
	objectBytes     map[string]int
}

// Bounded initial lists deliberately use classic list/watch. WatchList initial
// event streaming would bypass the collection-size guard.
type boundedListWatch struct{ *cache.ListWatch }

func (*boundedListWatch) IsWatchListSemanticsUnSupported() bool { return true }
func streamObjects(informer cache.SharedIndexInformer) []model.Object {
	out := []model.Object{}
	for _, v := range informer.GetStore().List() {
		if o, ok := v.(*unstructured.Unstructured); ok {
			out = append(out, model.CloneProjected(model.Object(o.Object)))
		}
	}
	sortObjects(out)
	return out
}
func (k *cluster) ensureStream(spec Kind, ns string, seed *unstructured.UnstructuredList) {
	key := listKey(spec, ns)
	k.mu.Lock()
	if k.watchCtx == nil || k.watchCtx.Err() != nil {
		k.mu.Unlock()
		return
	}
	if old := k.streams[key]; old != nil {
		if old.state == "watching" || old.state == "syncing" || time.Now().Before(old.retryAfter) {
			k.mu.Unlock()
			return
		}
		old.cancel()
	}
	if len(k.streams) >= 64 && k.streams[key] == nil {
		for oldKey, s := range k.streams {
			if s.retainedBytes == 0 && s.state != "watching" && s.state != "syncing" && s.state != "recovering" {
				delete(k.streams, oldKey)
			}
		}
		if len(k.streams) >= 64 {
			k.retentionWarningLocked(key, "Informer count limit reached; using bounded polling")
			k.mu.Unlock()
			return
		}
	}
	if old := k.streams[key]; old != nil && old.retainedBytes > 0 {
		k.mu.Unlock()
		return
	}
	delete(k.retentionWarnings, key)
	ctx, cancel := context.WithCancel(k.watchCtx)
	stream := &resourceStream{cancel: cancel, state: "syncing", kind: spec.Name, namespace: ns, lastUsed: time.Now(), objectBytes: map[string]int{}}
	k.streams[key] = stream
	k.workers.Add(1)
	k.mu.Unlock()
	var seedMu sync.Mutex
	initial := seed.DeepCopy()
	lw := &boundedListWatch{&cache.ListWatch{
		DisableChunking: true,
		ListWithContextFunc: func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			seedMu.Lock()
			first := initial
			initial = nil
			seedMu.Unlock()
			if first != nil {
				return first, nil
			}
			list, truncated, e := k.directList(ctx, spec, ns, opts)
			if e != nil {
				return nil, e
			}
			if truncated {
				return nil, &Error{Code: 413, Message: "Informer collection exceeds 5000 objects; using bounded polling"}
			}
			return list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			timeout := int64(30)
			opts.TimeoutSeconds = &timeout
			opts.AllowWatchBookmarks = true
			w, e := k.resource(spec, ns).Watch(quiet(ctx), opts)
			if e != nil {
				return nil, e
			}
			k.mu.Lock()
			if ctx.Err() == nil {
				stream.healthy = true
				stream.state = "watching"
				stream.message = ""
			}
			k.mu.Unlock()
			return k.monitorWatch(ctx, key, stream, w), nil
		},
	}}
	informer := cache.NewSharedIndexInformer(lw, &unstructured.Unstructured{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	// No Secret payload, annotation or literal env value enters an informer store.
	_ = informer.SetTransform(func(v any) (any, error) {
		o, ok := v.(*unstructured.Unstructured)
		if !ok {
			return v, nil
		}
		raw := model.Object{}
		for field, value := range o.Object {
			raw[field] = value
		}
		raw["kind"] = spec.Name
		clean := model.Sanitize(raw)
		data, _ := json.Marshal(clean)
		if !k.reserveStreamObject(key, stream, objectIdentity(clean), len(data)) {
			return nil, &Error{Code: 413, Message: "Informer retention budget reached"}
		}
		return &unstructured.Unstructured{Object: map[string]any(clean)}, nil
	})
	changed := func(delta int, namespace string) {
		k.mu.Lock()
		if k.streams[key] != stream || ctx.Err() != nil {
			k.mu.Unlock()
			return
		}
		stream.lastSync = time.Now().UTC()
		stream.objectCount = max(0, stream.objectCount+delta)
		if stream.objectCount > collectionLimit {
			stream.healthy = false
			stream.state = "limited"
			stream.message = "Collection limit reached; using bounded polling"
			stream.retryAfter = time.Now().Add(time.Minute)
			delete(k.cache, key)
			cancel()
		}
		notify := k.notify
		scoped := k.notifyScoped
		k.mu.Unlock()
		if scoped != nil {
			scoped(model.ResourceChange{Kind: stream.kind, Namespace: namespace})
		} else if notify != nil {
			notify()
		}
	}
	_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: func(v any) {
		namespace := ns
		if o, ok := v.(*unstructured.Unstructured); ok {
			namespace = o.GetNamespace()
		}
		changed(1, namespace)
	}, UpdateFunc: func(old, next any) {
		a, aok := old.(*unstructured.Unstructured)
		b, bok := next.(*unstructured.Unstructured)
		if !aok || !bok || a.GetResourceVersion() != b.GetResourceVersion() {
			namespace := ns
			if bok {
				namespace = b.GetNamespace()
			}
			changed(0, namespace)
		}
	}, DeleteFunc: func(v any) {
		if tombstone, ok := v.(cache.DeletedFinalStateUnknown); ok {
			v = tombstone.Obj
		}
		namespace := ns
		if o, ok := v.(*unstructured.Unstructured); ok {
			namespace = o.GetNamespace()
			k.mu.Lock()
			id := objectIdentity(model.Object(o.Object))
			stream.retainedBytes -= stream.objectBytes[id]
			delete(stream.objectBytes, id)
			k.mu.Unlock()
		}
		changed(-1, namespace)
	}})
	_ = informer.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *cache.Reflector, e error) {
		safe := safeError(e)
		k.mu.Lock()
		if k.streams[key] == stream {
			stream.healthy = false
			stream.state = "recovering"
			stream.message = safe.Error()
			delete(k.cache, key)
			if code := Status(safe); code == 401 || code == 403 || code == 404 || code == 413 {
				stream.state = "polling"
				stream.retryAfter = time.Now().Add(time.Minute)
				cancel()
			}
		}
		notify := k.notify
		scoped := k.notifyScoped
		k.mu.Unlock()
		if scoped != nil {
			scoped(model.ResourceChange{Kind: stream.kind, Namespace: stream.namespace})
		} else if notify != nil {
			notify()
		}
	})
	k.mu.Lock()
	stream.informer = informer
	k.mu.Unlock()
	go func() {
		defer k.workers.Done()
		informer.RunWithContext(quiet(ctx))
		seedMu.Lock()
		initial = nil
		seedMu.Unlock()
		_ = informer.GetStore().Replace(nil, "")
		k.mu.Lock()
		stream.retainedBytes = 0
		stream.objectBytes = nil
		stream.informer = nil
		if k.streams[key] == stream {
			stream.healthy = false
			if stream.state == "watching" || stream.state == "syncing" {
				stream.state = "stopped"
			}
			delete(k.cache, key)
		}
		k.mu.Unlock()
	}()
}
func (k *cluster) Watch(ctx context.Context, notify func()) { k.watch(ctx, notify, nil) }
func (k *cluster) WatchScoped(ctx context.Context, notify func(model.ResourceChange)) {
	k.watch(ctx, nil, notify)
}
func (k *cluster) watch(ctx context.Context, notify func(), scoped func(model.ResourceChange)) {
	k.mu.Lock()
	if k.watchCtx != nil {
		k.mu.Unlock()
		<-ctx.Done()
		return
	}
	k.watchCtx = ctx
	k.notify = notify
	k.notifyScoped = scoped
	k.mu.Unlock()
	// Readers start bounded informers; periodically release inactive state even
	// when the browser leaves and no further resource reads arrive.
	ticker := time.NewTicker(readLease)
	defer ticker.Stop()
watching:
	for {
		select {
		case <-ctx.Done():
			break watching
		case <-ticker.C:
			k.mu.Lock()
			k.expireRetentionLocked(time.Now())
			k.mu.Unlock()
		}
	}
	k.mu.Lock()
	k.watchCtx = nil
	k.notify = nil
	k.notifyScoped = nil
	for _, s := range k.streams {
		s.cancel()
	}
	k.mu.Unlock()
	k.workers.Wait()
	k.mu.Lock()
	clear(k.cache)
	clear(k.streams)
	k.mu.Unlock()
	utilnet.CloseIdleConnectionsFor(k.client.Transport)
}

// A normal watch close is observable too; cached state is not advertised as a
// live stream while client-go is reconnecting from its last resource version.
type monitoredWatch struct {
	source watch.Interface
	events chan watch.Event
	cancel context.CancelFunc
	once   sync.Once
}

func (w *monitoredWatch) Stop()                          { w.once.Do(func() { w.cancel(); w.source.Stop() }) }
func (w *monitoredWatch) ResultChan() <-chan watch.Event { return w.events }
func (k *cluster) monitorWatch(parent context.Context, key string, stream *resourceStream, source watch.Interface) watch.Interface {
	ctx, cancel := context.WithCancel(parent)
	w := &monitoredWatch{source: source, events: make(chan watch.Event), cancel: cancel}
	go func() {
		defer close(w.events)
		defer w.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-source.ResultChan():
				if !ok {
					if ctx.Err() == nil {
						k.mu.Lock()
						if k.streams[key] == stream && stream.state == "watching" {
							stream.healthy = false
							stream.state = "recovering"
							stream.message = "Watch reconnecting; current reads use polling"
							delete(k.cache, key)
						}
						notify := k.notify
						scoped := k.notifyScoped
						k.mu.Unlock()
						if scoped != nil {
							scoped(model.ResourceChange{Kind: stream.kind, Namespace: stream.namespace})
						} else if notify != nil {
							notify()
						}
					}
					return
				}
				select {
				case w.events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return w
}
