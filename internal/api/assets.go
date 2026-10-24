package api

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

const maxAssetBytes = 8 << 20

type assetRepresentation struct {
	data []byte
	etag string
}
type staticAsset struct {
	plain, gzip assetRepresentation
	contentType string
	status      int
}
type assetStore map[string]staticAsset

// Embedded files are immutable. Prepare bytes and representation-specific ETags
// once, and share them across workspace replacements instead of rereading/hashing.
func prepareAssets(files fs.FS) assetStore {
	if files == nil {
		return nil
	}
	out := assetStore{}
	_ = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			out[name] = staticAsset{status: http.StatusNotFound}
			return nil
		}
		if strings.HasSuffix(name, ".gz") {
			return nil
		}
		read := func(name string) (assetRepresentation, bool) {
			file, err := files.Open(name)
			if err != nil {
				return assetRepresentation{}, false
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, maxAssetBytes+1))
			if err != nil || len(data) > maxAssetBytes {
				return assetRepresentation{}, false
			}
			return assetRepresentation{data: data, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(data))}, true
		}
		plain, ok := read(name)
		if !ok {
			out[name] = staticAsset{status: http.StatusInternalServerError}
			return nil
		}
		typ := mime.TypeByExtension(path.Ext(name))
		if typ == "" {
			typ = "application/octet-stream"
		}
		asset := staticAsset{plain: plain, contentType: typ}
		if strings.HasPrefix(name, "assets/") && (strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css")) {
			asset.gzip, _ = read(name + ".gz")
		}
		out[name] = asset
		return nil
	})
	return out
}

func acceptsGzip(value string) bool {
	quality := func(parameters []string) float64 {
		q := 1.0
		for _, parameter := range parameters {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(key, "q") {
				return 0
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || !(parsed >= 0 && parsed <= 1) {
				return 0
			}
			q = parsed
		}
		return q
	}
	wildcard := false
	for _, encoding := range strings.Split(value, ",") {
		parts := strings.Split(encoding, ";")
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "gzip":
			return quality(parts[1:]) > 0
		case "*":
			wildcard = quality(parts[1:]) > 0
		}
	}
	return wildcard
}

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
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if !fs.ValidPath(name) {
		fail(w, 400, "Invalid path")
		return
	}
	asset, ok := a.assets[name]
	immutable := strings.HasPrefix(name, "assets/")
	if !ok && (immutable || strings.HasSuffix(name, ".gz")) {
		http.NotFound(w, r)
		return
	}
	if !ok {
		asset, ok = a.assets["index.html"]
		name = "index.html"
	}
	if !ok {
		fail(w, 503, "Frontend not built; run pnpm --dir web run build")
		return
	}
	if asset.status != 0 {
		if asset.status == http.StatusNotFound {
			http.NotFound(w, r)
		} else {
			fail(w, asset.status, "Asset read failed")
		}
		return
	}
	representation := asset.plain
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if asset.gzip.data != nil {
			w.Header().Add("Vary", "Accept-Encoding")
			// Range requests retain identity byte offsets. Ordinary GET and HEAD
			// negotiate the precompressed representation with its own validator.
			if r.Header.Get("Range") == "" && acceptsGzip(r.Header.Get("Accept-Encoding")) {
				representation = asset.gzip
				w.Header().Set("Content-Encoding", "gzip")
			}
		}
		w.Header().Set("ETag", representation.etag)
	}
	w.Header().Set("Content-Type", asset.contentType)
	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(representation.data))
}
