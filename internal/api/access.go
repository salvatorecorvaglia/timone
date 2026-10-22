package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"timone/internal/auth"
)

type accessKey struct{}
type accessState struct {
	identity  auth.Identity
	route     string
	requestID string
}

func markIdentity(r *http.Request, id auth.Identity) {
	if state, ok := r.Context().Value(accessKey{}).(*accessState); ok {
		state.identity = id
	}
}
func markRoute(r *http.Request) {
	if state, ok := r.Context().Value(accessKey{}).(*accessState); ok && r.Pattern != "" {
		state.route = r.Pattern
	}
}

type accessWriter struct {
	http.ResponseWriter
	status, bytes int
}

func (w *accessWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *accessWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *accessWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
func (w *accessWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (a *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 16)
		_, _ = rand.Read(raw)
		state := &accessState{requestID: hex.EncodeToString(raw), route: "unmatched"}
		w.Header().Set("X-Request-ID", state.requestID)
		r = r.WithContext(context.WithValue(r.Context(), accessKey{}, state))
		out := &accessWriter{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(out, r)
		if state.route == "unmatched" {
			markRoute(r)
		}
		if !a.config.AccessLog || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/mcp" && out.status < 400 {
			return
		}
		method := r.Method
		switch method {
		case "GET", "POST", "DELETE", "HEAD", "OPTIONS":
		default:
			method = "other"
		}
		actor := state.identity.Name
		if actor == "" {
			actor = "unauthenticated"
		}
		status := out.status
		if status == 0 {
			status = 200
		}
		a.log.LogAttrs(r.Context(), slog.LevelInfo, "access", slog.String("request_id", state.requestID), slog.String("method", method), slog.String("route", state.route), slog.String("actor", actor), slog.String("auth", state.identity.Method), slog.Int("status", status), slog.Int("bytes", out.bytes), slog.Int64("duration_ms", time.Since(start).Milliseconds()))
	})
}
