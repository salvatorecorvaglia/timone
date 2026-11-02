package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"timone/internal/service"
)

func investigationParameters(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	keys := map[string]bool{}
	for _, k := range allowed {
		keys[k] = true
	}
	for k, v := range r.URL.Query() {
		if !keys[k] || len(v) != 1 {
			fail(w, 400, "Unsupported or duplicate investigation parameter")
			return false
		}
	}
	return true
}
func (a *Server) incident(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "since", "until", "includeLogs") {
		return
	}
	q := r.URL.Query()
	window, e := service.Window(q.Get("since"), q.Get("until"), time.Now())
	if e != nil {
		respond(w, nil, e)
		return
	}
	include := q.Get("includeLogs")
	if include != "" && include != "true" && include != "false" {
		fail(w, 400, "Invalid includeLogs")
		return
	}
	ns := r.PathValue("namespace")
	if ns == "_" {
		ns = ""
	}
	v, e := a.service.Incident(r.Context(), r.PathValue("kind"), ns, r.PathValue("name"), window, include == "true")
	respond(w, v, e)
}
func (a *Server) workloadLogs(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "since", "until", "container", "tail", "previous", "sources") {
		return
	}
	q := r.URL.Query()
	window, e := service.Window(q.Get("since"), q.Get("until"), time.Now())
	if e != nil {
		respond(w, nil, e)
		return
	}
	tail := 100
	if q.Has("tail") {
		tail, e = strconv.Atoi(q.Get("tail"))
		if e != nil {
			fail(w, 400, "Invalid tail")
			return
		}
	}
	previous := q.Get("previous")
	if previous != "" && previous != "true" && previous != "false" {
		fail(w, 400, "Invalid previous")
		return
	}
	var selected []service.LogSourceRef
	if q.Has("sources") {
		raw := q.Get("sources")
		if len(raw) > 8192 {
			fail(w, 400, "Source selection exceeds 8 KiB")
			return
		}
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&selected) != nil || len(selected) == 0 || len(selected) > 12 {
			fail(w, 400, "Select 1–12 valid log sources")
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			fail(w, 400, "Invalid source selection")
			return
		}
	}
	v, e := a.service.WorkloadLogs(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"), q.Get("container"), tail, previous == "true", window, selected...)
	respond(w, v, e)
}
func (a *Server) storage(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r) {
		return
	}
	ns := r.PathValue("namespace")
	if ns == "_" {
		ns = ""
	}
	v, e := a.service.Storage(r.Context(), r.PathValue("kind"), ns, r.PathValue("name"))
	respond(w, v, e)
}

func (a *Server) focusedTopology(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "depth", "direction", "target") {
		return
	}
	q := r.URL.Query()
	depth := 1
	var e error
	if q.Has("depth") {
		depth, e = strconv.Atoi(q.Get("depth"))
		if e != nil {
			fail(w, 400, "Invalid exploration depth")
			return
		}
	}
	direction := q.Get("direction")
	if direction == "" {
		direction = "both"
	}
	ns := r.PathValue("namespace")
	if ns == "_" {
		ns = ""
	}
	v, e := a.service.FocusedTopology(r.Context(), r.PathValue("kind"), ns, r.PathValue("name"), depth, direction, q.Get("target"))
	respond(w, v, e)
}
func (a *Server) capture(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r) {
		return
	}
	ns := r.PathValue("namespace")
	if ns == "_" {
		ns = ""
	}
	v, e := a.service.Capture(r.Context(), r.PathValue("kind"), ns, r.PathValue("name"))
	respond(w, v, e)
}
func (a *Server) archivedHistory(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "namespace", "since", "until") {
		return
	}
	q := r.URL.Query()
	window, e := service.Window(q.Get("since"), q.Get("until"), time.Now())
	if e != nil {
		respond(w, nil, e)
		return
	}
	scope := service.Query{Namespace: q.Get("namespace")}
	if e := scope.Validate(); e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.ArchivedHistory(r.Context(), scope.Namespace, window)
	respond(w, v, e)
}

func (a *Server) logSources(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "container") {
		return
	}
	v, e := a.service.LogSources(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"), r.URL.Query().Get("container"))
	respond(w, v, e)
}

func (a *Server) scheduling(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r) {
		return
	}
	v, e := a.service.Scheduling(r.Context(), r.PathValue("namespace"), r.PathValue("name"))
	respond(w, v, e)
}
func (a *Server) rollout(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r) {
		return
	}
	v, e := a.service.Rollout(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"))
	respond(w, v, e)
}
func (a *Server) maintenance(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "namespace") {
		return
	}
	q := service.Query{Namespace: r.URL.Query().Get("namespace")}
	if e := q.Validate(); e != nil {
		respond(w, nil, e)
		return
	}
	v, e := a.service.NodeMaintenance(r.Context(), r.PathValue("name"), q.Namespace)
	respond(w, v, e)
}

func (a *Server) network(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "sourceNamespace", "sourcePod") {
		return
	}
	q := r.URL.Query()
	v, e := a.service.Network(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"), q.Get("sourceNamespace"), q.Get("sourcePod"))
	respond(w, v, e)
}
func (a *Server) batch(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r) {
		return
	}
	v, e := a.service.Batch(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"))
	respond(w, v, e)
}

func (a *Server) efficiency(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r, "range") {
		return
	}
	v, e := a.service.Efficiency(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"), r.URL.Query().Get("range"))
	respond(w, v, e)
}
func (a *Server) configuration(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r) {
		return
	}
	v, e := a.service.Configuration(r.Context(), r.PathValue("kind"), r.PathValue("namespace"), r.PathValue("name"))
	respond(w, v, e)
}

func (a *Server) termination(w http.ResponseWriter, r *http.Request) {
	if !investigationParameters(w, r) {
		return
	}
	ns := r.PathValue("namespace")
	if ns == "_" {
		ns = ""
	}
	v, e := a.service.Termination(r.Context(), r.PathValue("kind"), ns, r.PathValue("name"))
	respond(w, v, e)
}
