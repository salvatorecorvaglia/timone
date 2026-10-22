package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type loginWindow struct {
	at    time.Time
	count int
}
type loginLimiter struct {
	mu     sync.Mutex
	peers  map[netip.Addr]loginWindow
	global loginWindow
}

func inPrefixes(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}
func loginPeer(r *http.Request, trusted []netip.Prefix) netip.Addr {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	peer, _ := netip.ParseAddr(host)
	peer = peer.Unmap()
	if !inPrefixes(peer, trusted) {
		return peer
	}
	headers := r.Header.Values("X-Forwarded-For")
	if len(headers) != 1 || len(headers[0]) > 4096 {
		return peer
	}
	// Strip trusted hops from the right. Never accept a caller-selected leftmost IP.
	hops := strings.Split(headers[0], ",")
	for i := len(hops) - 1; i >= 0 && inPrefixes(peer, trusted); i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return peer
		}
		peer = ip.Unmap()
	}
	return peer
}
func (l *loginLimiter) allow(r *http.Request, trusted []netip.Prefix, now time.Time) bool {
	peer := loginPeer(r, trusted)
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.global.at) >= time.Minute {
		l.global = loginWindow{at: now}
	}
	// Independent global ceiling bounds a distributed flood, without allowing a
	// single peer's 20 failures to lock out every other viewer.
	if l.global.count >= 2000 {
		return false
	}
	if l.peers == nil {
		l.peers = map[netip.Addr]loginWindow{}
	}
	window := l.peers[peer]
	if now.Sub(window.at) >= time.Minute {
		window = loginWindow{at: now}
	}
	if window.count >= 20 {
		return false
	}
	if _, ok := l.peers[peer]; !ok && len(l.peers) >= 1024 {
		for ip, w := range l.peers {
			if now.Sub(w.at) >= time.Minute {
				delete(l.peers, ip)
			}
		}
		if len(l.peers) >= 1024 {
			return false
		}
	}
	// Requests rejected by the peer limit must not consume other peers' quota.
	l.global.count++
	window.count++
	l.peers[peer] = window
	return true
}
