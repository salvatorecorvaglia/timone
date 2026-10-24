package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

const CookieName = "timone_session"
const Lifetime = 8 * time.Hour
const MaxSessions = 1024
const MaxTokenBytes = 512

type Identity struct {
	Name   string `json:"name"`
	Method string `json:"method"`
}
type Options struct {
	Mode, Token, TokenFile string
	Anonymous              bool
	TrustedProxies         []netip.Prefix
}
type session struct {
	fingerprint [32]byte
	expires     time.Time
}
type Manager struct {
	options  Options
	mu       sync.Mutex
	sessions map[string]session
	now      func() time.Time
}

func New(options Options) *Manager {
	if options.Mode == "" {
		options.Mode = "token"
	}
	return &Manager{options: options, sessions: map[string]session{}, now: time.Now}
}
func ValidToken(token string) bool {
	if len(token) < 32 || len(token) > MaxTokenBytes {
		return false
	}
	for _, c := range token {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}
func ReadTokenFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("authentication token file unavailable")
	}
	defer f.Close()
	// Reserve two bytes for CRLF and one sentinel byte to detect excess input.
	b, err := io.ReadAll(io.LimitReader(f, MaxTokenBytes+3))
	if err != nil || len(b) > MaxTokenBytes+2 {
		return "", fmt.Errorf("authentication token file invalid")
	}
	token := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if !ValidToken(token) {
		return "", fmt.Errorf("authentication token file invalid")
	}
	return token, nil
}
func (m *Manager) current() (string, error) {
	if m.options.TokenFile != "" {
		return ReadTokenFile(m.options.TokenFile)
	}
	if m.options.Token == "" && m.options.Anonymous && m.options.Mode == "token" {
		return "", nil
	}
	if !ValidToken(m.options.Token) {
		return "", fmt.Errorf("authentication credentials unavailable")
	}
	return m.options.Token, nil
}
func (m *Manager) Authenticate(r *http.Request) (Identity, error) {
	token, err := m.current()
	if err != nil {
		return Identity{}, err
	}
	if m.options.Mode == "proxy" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return Identity{}, nil
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return Identity{}, nil
		}
		ip = ip.Unmap()
		trusted := false
		for _, prefix := range m.options.TrustedProxies {
			if prefix.Contains(ip) {
				trusted = true
				break
			}
		}
		// Forwarded client addresses are never used as evidence of proxy trust.
		if !trusted || len(r.Header.Values("X-Timone-Proxy-Token")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Timone-Proxy-Token")), []byte(token)) != 1 {
			return Identity{}, nil
		}
		users := r.Header.Values("X-Forwarded-User")
		if len(users) != 1 || len(users[0]) == 0 || len(users[0]) > 200 {
			return Identity{}, nil
		}
		for _, c := range users[0] {
			if c <= 32 || c == 127 || c == ',' || c == '\\' {
				return Identity{}, nil
			}
		}
		return Identity{Name: users[0], Method: "proxy"}, nil
	}
	if token == "" && m.options.Anonymous {
		return Identity{Name: "local", Method: "anonymous"}, nil
	}
	if headers := r.Header.Values("Authorization"); len(headers) > 0 {
		if len(headers) != 1 {
			return Identity{}, nil
		}
		parts := strings.Fields(headers[0])
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && subtle.ConstantTimeCompare([]byte(parts[1]), []byte(token)) == 1 {
			return Identity{Name: "administrator", Method: "bearer"}, nil
		}
		return Identity{}, nil
	}
	cookie, ok := sessionCookie(r)
	if !ok {
		return Identity{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[cookie]
	if !ok {
		return Identity{}, nil
	}
	if !m.now().Before(s.expires) || s.fingerprint != sha256.Sum256([]byte(token)) {
		delete(m.sessions, cookie)
		return Identity{}, nil
	}
	return Identity{Name: "administrator", Method: "session"}, nil
}
func sessionCookie(r *http.Request) (string, bool) {
	result := ""
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == CookieName {
			result = c.Value
			count++
		}
	}
	return result, count == 1 && len(result) == 43
}
func (m *Manager) Login(candidate string) (string, error) {
	if m.options.Mode != "token" {
		return "", fmt.Errorf("proxy authentication does not accept token login")
	}
	token, err := m.current()
	if err != nil {
		return "", err
	}
	if token == "" || subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) != 1 {
		return "", nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("session unavailable")
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for id, s := range m.sessions {
		if !now.Before(s.expires) || s.fingerprint != sha256.Sum256([]byte(token)) {
			delete(m.sessions, id)
		}
	}
	if len(m.sessions) >= MaxSessions {
		return "", fmt.Errorf("session capacity reached")
	}
	m.sessions[id] = session{fingerprint: sha256.Sum256([]byte(token)), expires: now.Add(Lifetime)}
	return id, nil
}
func (m *Manager) Revoke(r *http.Request) {
	id, ok := sessionCookie(r)
	if !ok {
		return
	}
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *Manager) Ready() bool { _, err := m.current(); return err == nil }
