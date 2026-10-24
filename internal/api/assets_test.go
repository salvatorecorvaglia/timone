package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"timone/internal/config"
	"timone/internal/provider"
	"timone/internal/service"
)

type countedAssets struct {
	fs.FS
	reads int
}

func (f *countedAssets) Open(name string) (fs.File, error) { f.reads++; return f.FS.Open(name) }

func TestStaticAssetNegotiationAndPreparedOwnership(t *testing.T) {
	plain := []byte(strings.Repeat("console.log('Timone');\n", 100))
	var compressed bytes.Buffer
	zip := gzip.NewWriter(&compressed)
	if _, err := zip.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	files := &countedAssets{FS: fstest.MapFS{
		"index.html":       {Data: []byte("<html>shell</html>")},
		"assets/app.js":    {Data: plain},
		"assets/app.js.gz": {Data: compressed.Bytes()},
	}}
	h := New(service.New(provider.NewDemo()), config.Config{Mode: "demo", Listen: "127.0.0.1:8080"}, files, NewBroker())
	reads := files.reads
	request := func(method, target, encoding, etag, byteRange string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8080"+target, nil)
		r.Header.Set("Accept-Encoding", encoding)
		r.Header.Set("If-None-Match", etag)
		r.Header.Set("Range", byteRange)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	identity := request("GET", "/assets/app.js", "", "", "")
	encoded := request("GET", "/assets/app.js", "br, gzip", "", "")
	if encoded.Code != 200 || encoded.Header().Get("Content-Encoding") != "gzip" || encoded.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatal(encoded.Code, encoded.Header())
	}
	reader, err := gzip.NewReader(encoded.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(decoded, plain) {
		t.Fatal("compressed bytes differ", err)
	}
	reader.Close()
	if identity.Header().Get("ETag") == encoded.Header().Get("ETag") {
		t.Fatal("representation validators must differ")
	}
	for _, encoding := range []string{"gzip;q=0, *;q=1", "br", "gzip;q=invalid", "gzip;q=NaN"} {
		w := request("GET", "/assets/app.js", encoding, "", "")
		if w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), plain) {
			t.Fatal("identity negotiation failed", encoding)
		}
	}
	head := request("HEAD", "/assets/app.js", "gzip", "", "")
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != encoded.Header().Get("Content-Length") {
		t.Fatal(head.Code, head.Header())
	}
	if w := request("GET", "/assets/app.js", "gzip", encoded.Header().Get("ETag"), ""); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("gzip revalidation failed", w.Code)
	}
	if w := request("GET", "/assets/app.js", "gzip", identity.Header().Get("ETag"), ""); w.Code != 200 {
		t.Fatal("cross-representation validator accepted", w.Code)
	}
	if w := request("GET", "/assets/app.js", "gzip", "", "bytes=0-6"); w.Code != 206 || w.Header().Get("Content-Encoding") != "" || w.Body.String() != string(plain[:7]) {
		t.Fatal("identity range failed", w.Code, w.Header())
	}
	if w := request("GET", "/resources", "gzip", "", ""); w.Code != 200 || w.Header().Get("Content-Encoding") != "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("shell caching changed", w.Code, w.Header())
	}
	if w := request("GET", "/api/v1/cluster", "gzip", "", ""); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("API compressed")
	}
	if w := request("GET", "/assets/missing.js", "gzip", "", ""); w.Code != 404 {
		t.Fatal("missing asset fell back to HTML")
	}
	if files.reads != reads {
		t.Fatalf("assets reread after startup: before=%d after=%d", reads, files.reads)
	}
}

func TestStaticAssetBoundsAndPlainFallback(t *testing.T) {
	files := fstest.MapFS{"assets/plain.css": {Data: []byte("body{}")}, "assets/large.js": {Data: make([]byte, maxAssetBytes+1)}}
	h := New(service.New(provider.NewDemo()), config.Config{Mode: "demo", Listen: "127.0.0.1:8080"}, files, NewBroker())
	for target, status := range map[string]int{"/assets/plain.css": 200, "/assets/large.js": 500, "/assets/plain.css.gz": 404} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8080"+target, nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status || w.Header().Get("Content-Encoding") != "" {
			t.Fatal(target, w.Code, w.Header())
		}
	}
}
