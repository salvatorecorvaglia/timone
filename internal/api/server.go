package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"timone/internal/auth"

	"timone/internal/config"
	"timone/internal/mcp"
	"timone/internal/provider"
	"timone/internal/service"
)

type Server struct {
	workspace   *Runtime
	service     *service.Service
	config      config.Config
	broker      *Broker
	assets      assetStore
	loginLimits loginLimiter
	authn       *auth.Manager
	log         *slog.Logger
}

func New(s *service.Service, c config.Config, assets fs.FS, b *Broker) http.Handler {
	return NewWithLogger(s, c, assets, b, slog.Default())
}
func NewWithLogger(s *service.Service, c config.Config, assets fs.FS, b *Broker, log *slog.Logger) http.Handler {
	return newWithRuntime(s, c, assets, b, log, nil, nil)
}
func newWithRuntime(s *service.Service, c config.Config, assets fs.FS, b *Broker, log *slog.Logger, runtime *Runtime, sharedAuth *auth.Manager) http.Handler {
	host, _, _ := net.SplitHostPort(c.Listen)
	ip := net.ParseIP(host)
	anonymous := c.Token == "" && c.TokenFile == "" && c.AuthMode != "proxy" && c.Mode != "incluster" && ip != nil && ip.IsLoopback()
	a := &Server{service: s, config: c, broker: b, log: log, authn: auth.New(auth.Options{Mode: c.AuthMode, Token: c.Token, TokenFile: c.TokenFile, TrustedProxies: c.TrustedProxies, Anonymous: anonymous})}
	if runtime != nil {
		a.assets = runtime.preparedAssets
	} else {
		a.assets = prepareAssets(assets)
	}
	a.workspace = runtime
	if sharedAuth != nil {
		a.authn = sharedAuth
	} else if runtime != nil {
		runtime.authn = a.authn
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]any{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !a.authn.Ready() {
			write(w, 503, map[string]any{"status": "authentication configuration unavailable"})
			return
		}
		write(w, 200, map[string]any{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/session", a.session)
	mux.HandleFunc("POST /api/v1/session", a.login)
	mux.HandleFunc("DELETE /api/v1/session", a.logout)
	mux.Handle("/api/v1/", a.auth(a.routes()))
	mux.Handle("/mcp", a.auth(mcp.Handler(s)))
	mux.HandleFunc("/", a.frontend)
	return a.observe(a.security(mux))
}
func write(w http.ResponseWriter, status int, v any) {
	b, e := json.Marshal(v)
	if e != nil {
		status = 500
		b = []byte(`{"error":{"code":500,"message":"Response encoding failed"}}`)
	}
	if len(b) > 8<<20 {
		status = 413
		b = []byte(`{"error":{"code":413,"message":"Response too large; narrow filters"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
func fail(w http.ResponseWriter, code int, message string) {
	write(w, code, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
func respond(w http.ResponseWriter, v any, e error) {
	if e != nil {
		fail(w, provider.Status(e), e.Error())
		return
	}
	write(w, 200, v)
}
func query(r *http.Request) (service.Query, error) {
	v := r.URL.Query()
	q := service.Query{Kind: v.Get("kind"), Namespace: v.Get("namespace"), Resource: v.Get("resource"), Since: v.Get("since"), Until: v.Get("until"), Search: v.Get("search"), Status: v.Get("status"), Phase: v.Get("phase"), Sort: v.Get("sort"), Desc: v.Get("desc") == "true"}
	for _, p := range []struct {
		key  string
		dest *int
	}{{"limit", &q.Limit}, {"offset", &q.Offset}} {
		if v.Has(p.key) {
			n, e := strconv.Atoi(v.Get(p.key))
			if e != nil || n < 0 || (p.key == "limit" && n == 0) {
				return q, &provider.Error{Code: 400, Message: "Invalid " + p.key}
			}
			*p.dest = n
		}
	}
	return q, q.Validate()
}
func (a *Server) overview(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.Overview(r.Context(), q.Namespace)
	respond(w, v, e)
}
func (a *Server) resources(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.List(r.Context(), q)
	respond(w, v, e)
}
func (a *Server) detail(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	if ns == "_" {
		ns = ""
	}
	v, e := a.service.Detail(r.Context(), r.PathValue("kind"), ns, r.PathValue("name"))
	respond(w, v, e)
}
func (a *Server) events(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.Events(r.Context(), q)
	respond(w, v, e)
}
func (a *Server) issues(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.Issues(r.Context(), q.Namespace)
	respond(w, v, e)
}
func (a *Server) issue(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.Issue(r.Context(), q.Namespace, r.PathValue("id"))
	respond(w, v, e)
}
func (a *Server) topology(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	if len(r.URL.Query()["group"]) > 1 {
		fail(w, 400, "Duplicate topology group")
		return
	}
	v, e := a.service.TopologyWithGroup(r.Context(), q, r.URL.Query().Get("group"))
	respond(w, v, e)
}
func (a *Server) metrics(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.Metrics(r.Context(), q.Namespace)
	respond(w, v, e)
}
func (a *Server) integrations(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.Integrations(r.Context(), q.Namespace)
	respond(w, v, e)
}
func (a *Server) audit(w http.ResponseWriter, r *http.Request) {
	q, e := query(r)
	if e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.Audit(r.Context(), q.Namespace)
	respond(w, v, e)
}
func (a *Server) logs(w http.ResponseWriter, r *http.Request) {
	tail := 100
	var e error
	if r.URL.Query().Has("tail") {
		tail, e = strconv.Atoi(r.URL.Query().Get("tail"))
		if e != nil {
			fail(w, 400, "Invalid tail")
			return
		}
	}
	v, e := a.service.Logs(r.Context(), r.PathValue("namespace"), r.PathValue("name"), r.URL.Query().Get("container"), tail)
	respond(w, map[string]any{"logs": v, "mode": a.service.Provider.Mode()}, e)
}
