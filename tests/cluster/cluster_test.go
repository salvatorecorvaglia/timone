package cluster_test

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
	"timone/internal/config"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
)

// This acceptance test never falls back to the user's default kubeconfig. It
// creates and removes only its own namespace on a deliberately named kind cluster.
func TestDisposableClusterReadAuthorization(t *testing.T) {
	path := os.Getenv("TIMONE_ACCEPTANCE_KUBECONFIG")
	if path == "" {
		t.Skip("set TIMONE_ACCEPTANCE_KUBECONFIG for disposable kind acceptance")
	}
	raw, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal("cannot load acceptance kubeconfig")
	}
	const contextName = "kind-timone-acceptance"
	if raw.CurrentContext != contextName || raw.Contexts[contextName] == nil || raw.Contexts[contextName].Cluster != "kind-timone-acceptance" {
		t.Fatal("acceptance requires the isolated kind-timone-acceptance context and cluster")
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, contextName, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		t.Fatal("invalid acceptance configuration")
	}
	endpoint, parseErr := url.Parse(cfg.Host)
	if parseErr != nil || endpoint.Scheme != "https" || (endpoint.Hostname() != "localhost" && (net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(endpoint.Hostname()).IsLoopback())) {
		t.Fatal("acceptance API must use HTTPS on loopback")
	}
	cfg.Timeout = 15 * time.Second
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal("cannot initialize acceptance client")
	}
	version, err := client.Discovery().ServerVersion()
	if err != nil || (version.Minor != "36" && version.Minor != "37") {
		t.Fatal("acceptance requires a real Kubernetes 1.36 or 1.37 API")
	}
	t.Logf("Real Kubernetes target: %s", version.GitVersion)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	const ns = "timone-acceptance"
	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"app.kubernetes.io/managed-by": "timone-acceptance"}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("cannot create fresh acceptance namespace; existing namespaces are never reused or removed")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := client.CoreV1().Namespaces().Delete(cleanup, ns, metav1.DeleteOptions{}); err != nil {
			t.Error("acceptance namespace cleanup failed")
		}
	})
	_, err = client.CoreV1().ServiceAccounts(ns).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "viewer"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("create viewer")
	}
	_, err = client.RbacV1().Roles(ns).Create(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "viewer"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch"}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("create read role")
	}
	_, err = client.RbacV1().RoleBindings(ns).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "viewer"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "viewer", Namespace: ns}}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "viewer"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("create binding")
	}
	// A scheduling gate avoids starting any container or downloading a workload image.
	_, err = client.CoreV1().Pods(ns).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pending-app"}, Spec: corev1.PodSpec{SchedulingGates: []corev1.PodSchedulingGate{{Name: "timone.test/acceptance"}}, Containers: []corev1.Container{{Name: "app", Image: "example.invalid/timone-acceptance:unused"}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("create gated fixture")
	}
	token, err := client.CoreV1().ServiceAccounts(ns).CreateToken(ctx, "viewer", &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: ptr.To(int64(600))}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("create bounded viewer token")
	}
	viewer := clientcmdapi.NewConfig()
	proxy := newFaultProxy(t, endpoint.Host)
	viewer.CurrentContext = "viewer"
	viewer.Clusters["acceptance"] = &clientcmdapi.Cluster{Server: "https://" + proxy.address(), TLSServerName: endpoint.Hostname(), CertificateAuthority: cfg.CAFile, CertificateAuthorityData: cfg.CAData}
	tokenPath := filepath.Join(t.TempDir(), "token")
	writeToken(t, tokenPath, token.Status.Token)
	viewer.AuthInfos["viewer"] = &clientcmdapi.AuthInfo{TokenFile: tokenPath}
	viewer.Contexts["viewer"] = &clientcmdapi.Context{Cluster: "acceptance", AuthInfo: "viewer", Namespace: ns}
	privatePath := filepath.Join(t.TempDir(), "viewer.yaml")
	data, err := clientcmd.Write(*viewer)
	if err != nil {
		t.Fatal("encode viewer")
	}
	if err = os.WriteFile(privatePath, data, 0600); err != nil {
		t.Fatal("save private viewer configuration")
	}
	p, err := provider.NewCluster(config.Config{Mode: "local", Kubeconfig: privatePath, Context: "viewer", Namespace: ns})
	if err != nil {
		t.Fatal("initialize Timone viewer")
	}
	svc := service.New(p)
	watchCtx, stopWatch := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); p.Watch(watchCtx, func() {}) }()
	t.Cleanup(func() {
		stopWatch()
		select {
		case <-watchDone:
		case <-time.After(5 * time.Second):
			t.Error("informer workers failed to stop after cancellation")
		}
	})
	eventually := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("acceptance deadline")
			case <-time.After(250 * time.Millisecond):
			}
		}
		t.Fatal(label)
	}
	eventually("read grant did not become observable", func() bool {
		page, e := svc.List(ctx, service.Query{Kind: "Pod", Namespace: ns})
		return e == nil && page.Total == 1 && page.Items[0].Name == "pending-app"
	})
	if _, e := svc.List(ctx, service.Query{Kind: "Secret", Namespace: ns}); provider.Status(e) != 403 {
		t.Fatalf("Secret read must be forbidden, status %d", provider.Status(e))
	}
	if _, e := svc.List(ctx, service.Query{Kind: "Pod", Namespace: "default"}); provider.Status(e) != 403 {
		t.Fatalf("cross-namespace read must be forbidden, status %d", provider.Status(e))
	}
	eventually("Pod informer did not establish a real watch", func() bool { return watching(p, ctx, ns) })
	proxy.disconnect(true)
	// Mutate through the independent admin connection while the viewer is cut off.
	pod, err := client.CoreV1().Pods(ns).Get(ctx, "pending-app", metav1.GetOptions{})
	if err != nil {
		t.Fatal("get fixture for reconnect")
	}
	pod.Labels = map[string]string{"timone.test/reconnected": "yes"}
	if _, err = client.CoreV1().Pods(ns).Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal("update fixture")
	}
	if _, e := p.Get(ctx, "Pod", ns, "pending-app"); e == nil {
		t.Fatal("disconnected fresh GET unexpectedly succeeded")
	}
	proxy.disconnect(false)
	eventually("informer did not recover after network interruption", func() bool {
		objects, _, e := p.List(ctx, "Pod", ns)
		return e == nil && len(objects) == 1 && model.Str(objects[0], "metadata", "labels", "timone.test/reconnected") == "yes" && watching(p, ctx, ns)
	})
	for _, tc := range []struct{ kind, state string }{{"Pod", "allowed"}, {"Secret", "denied"}} {
		review, e := p.(provider.AuthorizationProvider).ReviewAccess(ctx, model.AccessQuery{Kind: tc.kind, Namespace: ns, Verb: "get"})
		if e != nil || review.State != tc.state {
			t.Fatalf("actual self review for %s must be %s", tc.kind, tc.state)
		}
	}
	// Rotating to a separately issued, unprivileged SA token proves that the
	// existing client rereads the projected file; constructing a new client would not.
	if _, err = client.CoreV1().ServiceAccounts(ns).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "denied"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal("create rotation identity")
	}
	denied, err := client.CoreV1().ServiceAccounts(ns).CreateToken(ctx, "denied", &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: ptr.To(int64(600))}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("issue rotation token")
	}
	writeToken(t, tokenPath, denied.Status.Token)
	eventually("rotated credential did not produce a real RBAC denial", func() bool { _, e := p.Get(ctx, "Pod", ns, "pending-app"); return provider.Status(e) == 403 })
	renewed, err := client.CoreV1().ServiceAccounts(ns).CreateToken(ctx, "viewer", &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: ptr.To(int64(600))}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("renew viewer token")
	}
	writeToken(t, tokenPath, renewed.Status.Token)
	eventually("renewed ServiceAccount token did not recover", func() bool { _, e := p.Get(ctx, "Pod", ns, "pending-app"); return e == nil })
	// Export only safe API responses, never credentials, for the optional real
	// coverage browser pass in the matrix runner.
	exportCoverage(t, ctx, svc)
	if err = client.RbacV1().RoleBindings(ns).Delete(ctx, "viewer", metav1.DeleteOptions{}); err != nil {
		t.Fatal("revoke read binding")
	}
	eventually("revoked access still returned a cached collection", func() bool {
		_, e := svc.List(ctx, service.Query{Kind: "Pod", Namespace: ns})
		return provider.Status(e) == 403
	})
}
