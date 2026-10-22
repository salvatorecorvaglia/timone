package provider

import (
	"bytes"
	"io"
	"testing"
)

// Measure the existing cumulative watch-body cap before considering framing
// changes. Ordinary reconnection must never remove the response-size boundary.
func BenchmarkBoundedHighChurnWatchBody(b *testing.B) {
	data := bytes.Repeat([]byte(`{"type":"MODIFIED","object":{"metadata":{"resourceVersion":"123456789"}}}`+"\n"), 250000)
	b.ReportAllocs()
	b.SetBytes(16 << 20)
	b.ResetTimer()
	for b.Loop() {
		body := &limitedBody{ReadCloser: io.NopCloser(bytes.NewReader(data)), remaining: 16 << 20}
		n, err := io.Copy(io.Discard, body)
		if n != 16<<20 || Status(err) != 413 {
			b.Fatal("watch body cap bypassed", n, err)
		}
	}
}
