package provider

import (
	"context"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"net/url"
	"strconv"
	"strings"
	"timone/internal/model"
)

func (k *cluster) Metrics(ctx context.Context, ns string) (model.Metrics, error) {
	out := model.Metrics{Source: "metrics.k8s.io", Items: []model.Metric{}}
	if k.namespace != "" {
		if ns != "" && ns != k.namespace {
			return out, &Error{403, "Namespace is outside configured scope"}
		}
		ns = k.namespace
	}
	messages := []string{}
	successful := 0
	for _, kind := range []string{"nodes", "pods"} {
		path := "/apis/metrics.k8s.io/v1beta1/" + kind
		if kind == "pods" && ns != "" {
			path = "/apis/metrics.k8s.io/v1beta1/namespaces/" + url.PathEscape(ns) + "/pods"
		}
		var page struct{ Items []model.Object }
		if e := k.read(ctx, path, &page); e != nil {
			messages = append(messages, kind+": "+e.Error())
			continue
		}
		successful++
		for _, o := range page.Items {
			base := model.Metric{Name: model.Str(o, "metadata", "name"), Namespace: model.Str(o, "metadata", "namespace"), Timestamp: model.Str(o, "timestamp"), Kind: "Node"}
			appendSample := func(m model.Metric, usage map[string]any) {
				cpu, e1 := parseQuantity(model.Str(usage, "cpu"))
				mem, e2 := parseQuantity(model.Str(usage, "memory"))
				if e1 != nil || e2 != nil || cpu < 0 || mem < 0 {
					messages = append(messages, "Invalid or incomplete sample for "+m.Name+" omitted")
					return
				}
				m.CPU = cpu
				m.Memory = mem
				out.Items = append(out.Items, m)
			}
			if kind == "nodes" {
				appendSample(base, model.Map(o, "usage"))
			} else {
				for _, v := range model.Arr(o, "containers") {
					c, _ := v.(map[string]any)
					m := base
					m.Kind = "Pod"
					m.Container = model.Str(c, "name")
					appendSample(m, model.Map(c, "usage"))
				}
			}
		}
	}
	nodes, _, nodeErr := k.List(ctx, "Node", "")
	if nodeErr == nil {
		capacity := map[string]model.Object{}
		for _, n := range nodes {
			capacity[model.Str(n, "metadata", "name")] = n
		}
		for i := range out.Items {
			if out.Items[i].Kind == "Node" {
				n := capacity[out.Items[i].Name]
				out.Items[i].CPUCapacity = Quantity(model.Str(n, "status", "capacity", "cpu"))
				out.Items[i].MemoryCapacity = Quantity(model.Str(n, "status", "capacity", "memory"))
			}
		}
	}
	out.Available = successful > 0
	out.Message = "Current samples only; no persistent metric history."
	if len(messages) > 0 {
		out.Message += " Partial or unavailable collections: " + strings.Join(messages, "; ") + ". Install Metrics Server and grant get/list on metrics.k8s.io."
	}
	return out, ctx.Err()
}
func parseQuantity(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("missing quantity")
	}
	q, e := resource.ParseQuantity(s)
	if e != nil {
		return 0, fmt.Errorf("invalid quantity")
	}
	f := q.AsApproximateFloat64()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("invalid quantity")
	}
	return f, nil
}
func Quantity(s string) float64 { f, _ := parseQuantity(s); return f }
func (k *cluster) Logs(ctx context.Context, ns, pod, container string, tail int) (string, error) {
	if k.namespace != "" && ns != k.namespace {
		return "", &Error{403, "Namespace is outside configured scope"}
	}
	q := url.Values{"tailLines": {strconv.Itoa(tail)}, "limitBytes": {"131072"}, "timestamps": {"true"}}
	if container != "" {
		q.Set("container", container)
	}
	res, e := k.request(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pod)+"/log?"+q.Encode())
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 131072))
	return string(b), e
}
