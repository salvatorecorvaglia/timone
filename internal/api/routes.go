package api

import (
	"net/http"
	"strconv"
	"timone/internal/model"
	"timone/internal/provider"
)

func (a *Server) routes() http.Handler {
	s := a.service
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/workspace", a.workspaceInfo)
	api.HandleFunc("POST /api/v1/workspace", a.switchWorkspace)
	api.HandleFunc("GET /api/v1/cluster", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, clusterResponse{Name: s.Provider.Name(), Mode: s.Provider.Mode(), Scope: s.Provider.Namespace(), Kinds: provider.Kinds})
	})
	api.HandleFunc("GET /api/v1/connection", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.Provider.Connection(r.Context())) })
	api.HandleFunc("GET /api/v1/overview", a.overview)
	api.HandleFunc("GET /api/v1/applications", func(w http.ResponseWriter, r *http.Request) {
		q, e := query(r)
		if e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.Applications(r.Context(), q)
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		q, e := searchQuery(r)
		if e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.Search(r.Context(), q)
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/namespaces", func(w http.ResponseWriter, r *http.Request) {
		q, e := query(r)
		if e != nil {
			respond(w, nil, e)
			return
		}
		if !r.URL.Query().Has("limit") {
			q.Limit = 200
		}
		q.Kind = "Namespace"
		page, e := s.List(r.Context(), q)
		respond(w, page, e)
	})
	api.HandleFunc("GET /api/v1/resources", a.resources)
	api.HandleFunc("GET /api/v1/resources/{kind}/{namespace}/{name}", a.detail)
	api.HandleFunc("GET /api/v1/serviceaccounts/{namespace}/{name}/grants", a.serviceAccountGrants)
	api.HandleFunc("GET /api/v1/custom-resource-types", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.CustomTypes(r.Context())
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/custom-resources/{definition}", func(w http.ResponseWriter, r *http.Request) {
		q, e := query(r)
		if e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.CustomList(r.Context(), r.PathValue("definition"), r.URL.Query().Get("version"), q)
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/custom-resources/{definition}/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
		ns := r.PathValue("namespace")
		if ns == "_" {
			ns = ""
		}
		v, e := s.CustomDetail(r.Context(), r.PathValue("definition"), r.URL.Query().Get("version"), ns, r.PathValue("name"))
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/events", a.events)
	api.HandleFunc("GET /api/v1/changes", func(w http.ResponseWriter, r *http.Request) {
		q, e := query(r)
		if e != nil {
			respond(w, nil, e)
			return
		}
		v, e := s.Changes(r.Context(), q)
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/issues", a.issues)
	api.HandleFunc("GET /api/v1/issues/{id}", a.issue)
	api.HandleFunc("GET /api/v1/topology", a.topology)
	api.HandleFunc("GET /api/v1/metrics", a.metrics)
	api.HandleFunc("GET /api/v1/metrics/history/status", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.HistoryStatus()) })
	api.HandleFunc("GET /api/v1/metrics/history", func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		for key := range values {
			if key != "kind" && key != "namespace" && key != "name" && key != "container" && key != "range" {
				fail(w, 400, "Unsupported history parameter")
				return
			}
			if len(values[key]) != 1 {
				fail(w, 400, "Duplicate history parameter")
				return
			}
		}
		v, e := s.MetricHistory(r.Context(), model.HistoryQuery{Kind: values.Get("kind"), Namespace: values.Get("namespace"), Name: values.Get("name"), Container: values.Get("container"), Range: values.Get("range")})
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/gitops/{kind}/{namespace}/{name}/drift", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.GitOpsDrift(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"))
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/integrations", a.integrations)
	api.HandleFunc("GET /api/v1/helm/releases/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.HelmHistory(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/helm/releases/{namespace}/{name}/compare", func(w http.ResponseWriter, r *http.Request) {
		from, e1 := strconv.Atoi(r.URL.Query().Get("from"))
		to, e2 := strconv.Atoi(r.URL.Query().Get("to"))
		if e1 != nil || e2 != nil {
			fail(w, 400, "from and to must be revision numbers")
			return
		}
		v, e := s.HelmCompare(r.Context(), r.PathValue("namespace"), r.PathValue("name"), from, to)
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/authorization", func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		for key, v := range values {
			switch key {
			case "kind", "namespace", "name", "verb", "subresource", "serviceAccount", "serviceAccountNamespace":
			default:
				fail(w, 400, "Unsupported authorization parameter")
				return
			}
			if len(v) != 1 {
				fail(w, 400, "Duplicate authorization parameter")
				return
			}
		}
		v, e := s.ReviewAccess(r.Context(), model.AccessQuery{Kind: values.Get("kind"), Namespace: values.Get("namespace"), Name: values.Get("name"), Verb: values.Get("verb"), Subresource: values.Get("subresource"), ServiceAccount: values.Get("serviceAccount"), ServiceAccountNamespace: values.Get("serviceAccountNamespace")})
		respond(w, v, e)
	})
	api.HandleFunc("GET /api/v1/audit", a.audit)
	api.HandleFunc("GET /api/v1/pods/{namespace}/{name}/logs", a.logs)
	api.HandleFunc("GET /api/v1/stream", a.stream)

	return api
}
