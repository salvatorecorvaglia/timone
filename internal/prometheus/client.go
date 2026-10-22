// Package prometheus queries a single administrator-configured endpoint using
// fixed resource selectors. No caller can submit a URL, PromQL or arbitrary labels.
package prometheus

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"timone/internal/model"
)

const MaxBody = 4 << 20
const MaxSeries = 64
const MaxPoints = 241

type Options struct {
	URL, TokenFile, CAFile, Cluster string
	AllowHTTP                       bool
}

func Validate(o Options) error {
	if o.URL == "" {
		if o.TokenFile != "" || o.CAFile != "" || o.Cluster != "" || o.AllowHTTP {
			return fmt.Errorf("Prometheus options require TIMONE_PROMETHEUS_URL")
		}
		return nil
	}
	u, err := url.Parse(o.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.ContainsAny(o.URL, "#%") || u.RawPath != "" || strings.Contains(u.Path, "..") || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("Prometheus URL must be an HTTP(S) base URL without credentials, query, fragment or escaped path")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid Prometheus port")
		}
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if !o.AllowHTTP && u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("HTTP Prometheus endpoints outside loopback require explicit TIMONE_PROMETHEUS_ALLOW_HTTP=true")
		}
		if o.TokenFile != "" {
			return fmt.Errorf("Prometheus bearer credentials require HTTPS")
		}
		if o.CAFile != "" {
			return fmt.Errorf("Prometheus CA configuration requires HTTPS")
		}
	}
	if len(o.Cluster) > 200 || strings.ContainsAny(o.Cluster, "\r\n\x00") {
		return fmt.Errorf("invalid Prometheus cluster label value")
	}
	return nil
}

type Client struct {
	options  Options
	client   *http.Client
	endpoint string
	slots    chan struct{}
}

func New(o Options) (*Client, error) {
	if err := Validate(o); err != nil {
		return nil, err
	}
	if o.URL == "" {
		return nil, nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.CAFile != "" {
		f, err := os.Open(o.CAFile)
		if err != nil {
			return nil, fmt.Errorf("Prometheus CA file unavailable")
		}
		b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if err != nil || len(b) > 1<<20 {
			return nil, fmt.Errorf("Prometheus CA file invalid")
		}
		roots, _ := x509.SystemCertPool()
		if roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("Prometheus CA file invalid")
		}
		tlsConfig.RootCAs = roots
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4, MaxIdleConns: 4, ResponseHeaderTimeout: 8 * time.Second, IdleConnTimeout: 30 * time.Second, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{options: o, client: client, endpoint: strings.TrimRight(o.URL, "/") + "/api/v1/query_range", slots: make(chan struct{}, 4)}, nil
}
func (c *Client) Status() model.HistoryCapability {
	return model.HistoryCapability{State: "configured", Source: "prometheus", Message: "An explicit Prometheus endpoint is configured. Availability is established only by resource queries; Service discovery is independent."}
}
func (c *Client) selector(t model.HistoryTarget) string {
	labels := []string{}
	if c.options.Cluster != "" {
		labels = append(labels, "cluster="+strconv.Quote(c.options.Cluster))
	}
	if t.Kind == "Node" {
		labels = append(labels, "node="+strconv.Quote(t.Name))
	} else {
		labels = append(labels, "namespace="+strconv.Quote(t.Namespace), "pod="+strconv.Quote(t.Name))
		if t.Container != "" {
			labels = append(labels, "container="+strconv.Quote(t.Container))
		} else {
			names := []string{}
			for _, name := range t.Containers {
				names = append(names, regexp.QuoteMeta(name))
			}
			sort.Strings(names)
			labels = append(labels, "container=~"+strconv.Quote(strings.Join(names, "|")))
		}
	}
	return strings.Join(labels, ",")
}
func (c *Client) expressions(t model.HistoryTarget) [2]string {
	selector := c.selector(t)
	if t.Kind == "Node" {
		return [2]string{"sum without (cpu, mode) (1 - rate(node_cpu_seconds_total{" + selector + ",mode=\"idle\"}[5m]))", "node_memory_MemTotal_bytes{" + selector + "} - node_memory_MemAvailable_bytes{" + selector + "}"}
	}
	return [2]string{"rate(container_cpu_usage_seconds_total{" + selector + "}[5m])", "container_memory_working_set_bytes{" + selector + "}"}
}
func tokenFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("Prometheus credential file unavailable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(b) > 16384 {
		return "", fmt.Errorf("Prometheus credential file invalid")
	}
	token := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if token == "" {
		return "", fmt.Errorf("Prometheus credential file invalid")
	}
	for _, r := range token {
		if r <= 32 || r >= 127 {
			return "", fmt.Errorf("Prometheus credential file invalid")
		}
	}
	return token, nil
}

type wireSeries struct {
	Metric map[string]string   `json:"metric"`
	Values [][]json.RawMessage `json:"values"`
}

func (c *Client) query(ctx context.Context, t model.HistoryTarget, expression, metric, unit string) ([]model.HistorySeries, []model.Warning, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	q := url.Values{"query": {expression}, "start": {strconv.FormatInt(t.Start.Unix(), 10)}, "end": {strconv.FormatInt(t.End.Unix(), 10)}, "step": {strconv.FormatInt(t.Step, 10)}, "timeout": {"8s"}, "limit": {"65"}, "lookback_delta": {"5m"}}
	req, err := http.NewRequestWithContext(ctx, "GET", c.endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("Prometheus request configuration invalid")
	}
	req.Header.Set("Accept", "application/json")
	if c.options.TokenFile != "" {
		token, err := tokenFile(c.options.TokenFile)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("Prometheus query failed; check connectivity, TLS and timeout")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		switch response.StatusCode {
		case 401, 403:
			return nil, nil, fmt.Errorf("Prometheus authentication or query permission denied")
		case 429:
			return nil, nil, fmt.Errorf("Prometheus query rate limit exceeded")
		}
		return nil, nil, fmt.Errorf("Prometheus range query unavailable")
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, MaxBody+1))
	if err != nil || len(b) > MaxBody {
		return nil, nil, fmt.Errorf("Prometheus response exceeds bounds or could not be read")
	}
	var envelope struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Infos    []string `json:"infos"`
		Data     struct {
			ResultType string       `json:"resultType"`
			Result     []wireSeries `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &envelope) != nil || envelope.Status != "success" || envelope.Data.ResultType != "matrix" {
		return nil, nil, fmt.Errorf("Prometheus returned an invalid or unsuccessful range query")
	}
	if len(envelope.Data.Result) > MaxSeries {
		return nil, nil, fmt.Errorf("Prometheus series limit exceeded; narrow the container selection")
	}
	warnings := []model.Warning{}
	if len(envelope.Warnings) > 0 || len(envelope.Infos) > 0 {
		warnings = append(warnings, model.Warning{Kind: metric, Message: "Prometheus reported query annotations; results may be incomplete. Raw upstream messages are omitted."})
	}
	allowed := map[string]bool{}
	for _, name := range t.Containers {
		allowed[name] = true
	}
	seen := map[string]bool{}
	out := []model.HistorySeries{}
	invalid := false
	for _, row := range envelope.Data.Result {
		if c.options.Cluster != "" && row.Metric["cluster"] != c.options.Cluster {
			return nil, nil, fmt.Errorf("Prometheus result does not match the configured cluster label")
		}
		container := ""
		if t.Kind == "Node" {
			if row.Metric["node"] != t.Name {
				return nil, nil, fmt.Errorf("Prometheus result labels do not match the authorized Node")
			}
		} else {
			container = row.Metric["container"]
			if row.Metric["namespace"] != t.Namespace || row.Metric["pod"] != t.Name || !allowed[container] || (t.Container != "" && container != t.Container) {
				return nil, nil, fmt.Errorf("Prometheus result labels do not match the authorized Pod/container")
			}
		}
		if seen[container] {
			return nil, nil, fmt.Errorf("Ambiguous duplicate Prometheus series; use a dedicated cluster/collector endpoint or narrower scrape configuration")
		}
		seen[container] = true
		if len(row.Values) > MaxPoints {
			return nil, nil, fmt.Errorf("Prometheus sample limit exceeded")
		}
		series := model.HistorySeries{Metric: metric, Unit: unit, Container: container, Points: []model.HistoryPoint{}}
		last := int64(-1)
		for _, pair := range row.Values {
			if len(pair) != 2 {
				return nil, nil, fmt.Errorf("Prometheus returned malformed samples")
			}
			var timestamp float64
			var text string
			if json.Unmarshal(pair[0], &timestamp) != nil || math.IsNaN(timestamp) || math.IsInf(timestamp, 0) || timestamp != math.Trunc(timestamp) || timestamp < float64(t.Start.Unix()) || timestamp > float64(t.End.Unix()) || int64(timestamp) <= last || (int64(timestamp)-t.Start.Unix())%t.Step != 0 || json.Unmarshal(pair[1], &text) != nil {
				return nil, nil, fmt.Errorf("Prometheus returned invalid sample timestamps or values")
			}
			point := model.HistoryPoint{Timestamp: int64(timestamp)}
			last = point.Timestamp
			v, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				invalid = true
			} else {
				point.Value = &v
			}
			series.Points = append(series.Points, point)
		}
		out = append(out, series)
	}
	if invalid {
		warnings = append(warnings, model.Warning{Kind: metric, Message: "Invalid or non-finite samples remain null; missing values are never replaced with zero."})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Container < out[j].Container })
	return out, warnings, nil
}
func (c *Client) Query(ctx context.Context, t model.HistoryTarget) (model.HistoryData, error) {
	if (t.Kind != "Pod" && t.Kind != "Node") || t.Step < 60 || t.End.Before(t.Start) || t.End.Sub(t.Start) > 24*time.Hour || int64(t.End.Sub(t.Start).Seconds())/t.Step+1 > MaxPoints || (t.Kind == "Pod" && (len(t.Containers) == 0 || len(t.Containers) > MaxSeries)) {
		return model.HistoryData{}, fmt.Errorf("Invalid bounded Prometheus resource target")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	type outcome struct {
		series   []model.HistorySeries
		warnings []model.Warning
		err      error
	}
	results := make(chan outcome, 2)
	expressions := c.expressions(t)
	for i, spec := range []struct{ metric, unit string }{{"cpu", "cores"}, {"memory", "bytes"}} {
		go func(expression, metric, unit string) {
			s, w, e := c.query(ctx, t, expression, metric, unit)
			if e != nil {
				w = append(w, model.Warning{Kind: metric, Message: e.Error()})
			}
			results <- outcome{s, w, e}
		}(expressions[i], spec.metric, spec.unit)
	}
	out := model.HistoryData{State: "empty", Message: "No matching historical samples. Check collection, retention and required resource labels.", Series: []model.HistorySeries{}, Warnings: []model.Warning{}}
	failures := 0
	for range 2 {
		result := <-results
		out.Series = append(out.Series, result.series...)
		out.Warnings = append(out.Warnings, result.warnings...)
		if result.err != nil {
			failures++
		}
	}
	if ctx.Err() != nil && len(out.Series) == 0 {
		return out, ctx.Err()
	}
	valid := 0
	coverage := map[string]bool{}
	for _, s := range out.Series {
		for _, p := range s.Points {
			if p.Value != nil {
				valid++
				coverage[s.Metric+":"+s.Container] = true
			}
		}
	}
	containers := []string{""}
	if t.Kind == "Pod" {
		containers = t.Containers
		if t.Container != "" {
			containers = []string{t.Container}
		}
	}
	missing := false
	for _, metric := range []string{"cpu", "memory"} {
		for _, container := range containers {
			if !coverage[metric+":"+container] {
				missing = true
			}
		}
	}
	if missing {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Coverage", Message: "At least one requested metric/container has no usable samples. Missing history is unavailable, never zero."})
	}
	if valid > 0 {
		out.State = "available"
		out.Message = "Historical measurements from the configured Prometheus endpoint."
		if failures > 0 || len(out.Warnings) > 0 || missing {
			out.State = "partial"
			out.Message = "Some historical measurements are unavailable or incomplete; review coverage warnings."
		}
	} else if failures > 0 {
		out.State = "unavailable"
		out.Message = "Prometheus history could not be read. Review integration warnings."
	}
	sort.Slice(out.Series, func(i, j int) bool {
		return out.Series[i].Metric+out.Series[i].Container < out.Series[j].Metric+out.Series[j].Container
	})
	sort.Slice(out.Warnings, func(i, j int) bool {
		return out.Warnings[i].Kind+out.Warnings[i].Message < out.Warnings[j].Kind+out.Warnings[j].Message
	})
	return out, nil
}
