package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"timone/internal/auth"
	"timone/internal/provider"
)

func (a *Server) rejectAuthentication(w http.ResponseWriter, err error) {
	if err != nil {
		fail(w, 503, "Authentication credentials temporarily unavailable")
		return
	}
	if a.config.AuthMode == "proxy" {
		w.Header().Set("X-Timone-Auth-Required", "proxy")
		fail(w, 401, "Authentication through the trusted identity proxy is required")
		return
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	fail(w, 401, "Authentication required")
}
func (a *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := a.authn.Authenticate(r)
		if err != nil || identity.Name == "" {
			a.rejectAuthentication(w, err)
			return
		}
		markIdentity(r, identity)
		if r.URL.Path != "/api/v1/stream" && r.URL.Path != "/mcp" {
			release, ok := a.service.TryAdmit()
			if !ok {
				w.Header().Set("Retry-After", "1")
				fail(w, 429, "Investigation capacity reached; retry shortly")
				return
			}
			defer release()
		}
		r = r.WithContext(provider.WithReadBudget(r.Context(), provider.InvestigationBytes))
		next.ServeHTTP(w, r)
		markRoute(r)
	})
}
func (a *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		host, _, _ := net.SplitHostPort(a.config.Listen)
		ip := net.ParseIP(host)
		if ip != nil && ip.IsLoopback() && a.config.AuthMode != "proxy" {
			requestHost := r.Host
			if h, _, e := net.SplitHostPort(requestHost); e == nil {
				requestHost = h
			}
			if requestHost != "localhost" && requestHost != "127.0.0.1" && requestHost != "::1" {
				fail(w, 403, "Host not allowed")
				return
			}
		}
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
			if a.config.PublicURL != "" && (ip == nil || !ip.IsLoopback() || a.config.AuthMode == "proxy") {
				u, e := url.Parse(a.config.PublicURL)
				actualHost := r.Host
				if u != nil && u.Scheme == "https" {
					actualHost = strings.TrimSuffix(actualHost, ":443")
				} else {
					actualHost = strings.TrimSuffix(actualHost, ":80")
				}
				if e != nil || !strings.EqualFold(actualHost, u.Host) {
					fail(w, 403, "Host does not match configured public origin")
					return
				}
			}

			origin := r.Header.Get("Origin")
			if origin != "" {
				u, e := url.Parse(origin)
				expected := "http://" + r.Host
				if r.TLS != nil {
					expected = "https://" + r.Host
				}
				if a.config.PublicURL != "" {
					expected = strings.TrimRight(a.config.PublicURL, "/")
				}
				if e != nil || u.User != nil || origin != expected {
					fail(w, 403, "Cross-origin access denied")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "Cross-site access denied")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if r.URL.Path != "/api/v1/stream" {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Second))
			ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}
func (a *Server) session(w http.ResponseWriter, r *http.Request) {
	identity, err := a.authn.Authenticate(r)
	if err != nil {
		fail(w, 503, "Authentication credentials temporarily unavailable")
		return
	}
	markIdentity(r, identity)
	mode := a.config.AuthMode
	if mode == "" {
		mode = "token"
	}
	write(w, 200, sessionResponse{Authenticated: identity.Name != "", AuthRequired: a.config.Token != "" || a.config.TokenFile != "" || mode == "proxy", Mode: mode, Identity: identity})
}
func (a *Server) login(w http.ResponseWriter, r *http.Request) {
	if a.config.AuthMode == "proxy" {
		fail(w, 405, "Sign in through the configured identity proxy")
		return
	}
	if !a.loginLimits.allow(r, a.config.LoginTrustedProxies, time.Now()) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "Too many login attempts; retry in one minute")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil || dec.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "Invalid login body")
		return
	}
	id, err := a.authn.Login(body.Token)
	if err != nil {
		fail(w, 503, "Authentication credentials or session capacity unavailable")
		return
	}
	if id == "" {
		fail(w, 401, "Invalid access token")
		return
	}
	a.authn.Revoke(r)
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(a.config.PublicURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: int(auth.Lifetime.Seconds())})
	markIdentity(r, auth.Identity{Name: "administrator", Method: "session"})
	write(w, 200, authenticationResult{Authenticated: true})
}
func (a *Server) logout(w http.ResponseWriter, r *http.Request) {
	if a.config.AuthMode == "proxy" {
		fail(w, 405, "Sign out through the configured identity proxy")
		return
	}
	a.authn.Revoke(r)
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(a.config.PublicURL, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: -1})
	write(w, 200, authenticationResult{Authenticated: false})
}
