package api

import (
	"net/http"
	"net/url"
	"strconv"
	"timone/internal/provider"
	"timone/internal/service"
)

func (a *Server) serviceAccountGrants(w http.ResponseWriter, r *http.Request) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		respond(w, nil, &provider.Error{Code: 400, Message: "Invalid grant query encoding"})
		return
	}
	for key, v := range values {
		if (key != "limit" && key != "offset") || len(v) != 1 {
			respond(w, nil, &provider.Error{Code: 400, Message: "Unsupported or repeated grant parameter"})
			return
		}
	}
	q := service.GrantQuery{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
	for _, p := range []struct {
		key  string
		dest *int
	}{{"limit", &q.Limit}, {"offset", &q.Offset}} {
		if values.Has(p.key) {
			n, e := strconv.Atoi(values.Get(p.key))
			if e != nil || n < 0 || (p.key == "limit" && n == 0) {
				respond(w, nil, &provider.Error{Code: 400, Message: "Invalid " + p.key})
				return
			}
			*p.dest = n
		}
	}
	v, err := a.service.ServiceAccountGrants(r.Context(), q)
	respond(w, v, err)
}
