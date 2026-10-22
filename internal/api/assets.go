package api

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

func (a *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "Method not allowed")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		fail(w, 404, "API endpoint not found")
		return
	}
	if a.assets == nil {
		fail(w, 503, "Frontend not built; run pnpm --dir web run build")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if !fs.ValidPath(path) {
		fail(w, 400, "Invalid path")
		return
	}
	file, e := a.assets.Open(path)
	if e != nil {
		if strings.HasPrefix(path, "assets/") {
			http.NotFound(w, r)
			return
		}
		file, e = a.assets.Open("index.html")
	}
	if e != nil {
		fail(w, 503, "Frontend not built; run pnpm --dir web run build")
		return
	}
	defer file.Close()
	stat, e := file.Stat()
	if e != nil {
		fail(w, 500, "Asset metadata unavailable")
		return
	}
	if stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	b, e := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if e != nil || len(b) > 8<<20 {
		fail(w, 500, "Asset read failed")
		return
	}
	if strings.HasSuffix(path, ".js") {
		w.Header().Set("Content-Type", "text/javascript")
	}
	if strings.HasSuffix(path, ".css") {
		w.Header().Set("Content-Type", "text/css")
	}
	if !strings.HasPrefix(path, "assets/") || strings.HasSuffix(path, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	if strings.HasPrefix(path, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		digest := sha256.Sum256(b)
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, digest))
	}
	http.ServeContent(w, r, stat.Name(), time.Time{}, bytes.NewReader(b))
}
