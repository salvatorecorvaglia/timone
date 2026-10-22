package integrations

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"timone/internal/model"
)

type DesiredOptions struct{ ArgoURL, ArgoTokenFile, RenderedURL, RenderedTokenFile, CAFile, Destination string }

func (o DesiredOptions) Validate() error {
	for _, raw := range []string{o.ArgoURL, o.RenderedURL} {
		if raw == "" {
			continue
		}
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("GitOps desired endpoints require explicit HTTPS base URLs")
		}
	}
	if (o.ArgoTokenFile != "" && o.ArgoURL == "") || (o.RenderedTokenFile != "" && o.RenderedURL == "") {
		return fmt.Errorf("GitOps credential files require their endpoint")
	}
	return nil
}

var desiredSlots = make(chan struct{}, 4)

func DesiredJSON(ctx context.Context, base, path, tokenFile, caFile string, out any, transports ...http.RoundTripper) error {
	select {
	case desiredSlots <- struct{}{}:
		defer func() { <-desiredSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if base == "" {
		return fmt.Errorf("Desired manifest source is not configured")
	}
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return fmt.Errorf("Invalid desired manifest endpoint")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		file, e := os.Open(caFile)
		if e != nil {
			return fmt.Errorf("GitOps CA unavailable")
		}
		data, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		_ = file.Close()
		if e != nil || len(data) > 1<<20 {
			return fmt.Errorf("GitOps CA unavailable")
		}
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(data) {
			return fmt.Errorf("Invalid GitOps CA")
		}
		tlsConfig.RootCAs = pool
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, MaxConnsPerHost: 4, ResponseHeaderTimeout: 10 * time.Second}
	defer transport.CloseIdleConnections()
	var roundTripper http.RoundTripper = transport
	if len(transports) > 0 && transports[0] != nil {
		roundTripper = transports[0]
	}
	client := &http.Client{Transport: roundTripper, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if e != nil {
		return fmt.Errorf("Invalid desired request")
	}
	req.Header.Set("Accept", "application/json")
	if tokenFile != "" {
		file, e := os.Open(tokenFile)
		if e != nil {
			return fmt.Errorf("GitOps credential unavailable")
		}
		data, e := io.ReadAll(io.LimitReader(file, 8193))
		_ = file.Close()
		token := strings.TrimSpace(string(data))
		if e != nil || len(token) == 0 || len(data) > 8192 || strings.ContainsAny(token, "\r\n\t ") {
			return fmt.Errorf("Invalid GitOps credential")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, e := client.Do(req)
	if e != nil {
		return fmt.Errorf("Desired manifest service unreachable or TLS verification failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Desired manifest service returned HTTP %d", response.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if e != nil || len(data) > 4<<20 {
		return fmt.Errorf("Desired manifest response exceeds limits")
	}
	if json.Unmarshal(data, out) != nil {
		return fmt.Errorf("Invalid desired manifest response")
	}
	return ctx.Err()
}
func SafeManifest(o model.Object) (model.Object, bool, error) {
	data, e := json.Marshal(o)
	if e != nil || len(data) > 1<<20 {
		return nil, false, fmt.Errorf("Manifest exceeds comparison limit")
	}
	projected, e := helmConfiguration(context.Background(), string(data))
	if e != nil || len(projected) != 1 {
		return nil, false, fmt.Errorf("Cannot safely project manifest")
	}
	return projected[0].Object, projected[0].Supported, nil
}
