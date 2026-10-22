package api

import (
	"net/http"
	"net/url"
	"strconv"
	"timone/internal/provider"
	"timone/internal/service"
)

func searchQuery(r *http.Request) (service.SearchQuery, error) {
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return service.SearchQuery{}, &provider.Error{Code: 400, Message: "Invalid search query encoding"}
	}
	allowed := map[string]bool{"search": true, "namespace": true, "kind": true, "definition": true, "status": true, "limit": true, "offset": true}
	for key, values := range v {
		if !allowed[key] || len(values) != 1 {
			return service.SearchQuery{}, &provider.Error{Code: 400, Message: "Unsupported or repeated search parameter"}
		}
	}
	q := service.SearchQuery{Search: v.Get("search"), Namespace: v.Get("namespace"), Kind: v.Get("kind"), Definition: v.Get("definition"), Status: v.Get("status")}
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
