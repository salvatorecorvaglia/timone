package provider

import (
	"context"
	"sort"
	"strings"
	"time"
	"timone/internal/model"
)

func (d *demo) Connection(context.Context) model.Connection {
	return model.Connection{State: "simulated", Mode: "demo", Context: d.Name(), Auth: "none", Discovery: "simulated", Message: "Isolated demo data. No Kubernetes API connection is made.", CheckedAt: time.Now().UTC(), CollectionFailures: []model.Warning{}, Streams: []model.StreamStatus{}}
}
func (k *cluster) Connection(ctx context.Context) model.Connection {
	now := time.Now().UTC()
	c := model.Connection{Mode: "cluster", Context: k.name, Namespace: k.namespace, Auth: k.auth, Discovery: "client-go discovery", CheckedAt: now, CollectionFailures: []model.Warning{}, Streams: []model.StreamStatus{}}
	var version struct {
		GitVersion string `json:"gitVersion"`
	}
	e := k.read(ctx, "/version", &version)
	if e == nil {
		c.State = "connected"
		c.ServerVersion = version.GitVersion
		c.Message = "Kubernetes API is reachable. Collection permissions are reported separately."
	} else {
		c.State = "disconnected"
		if Status(e) == 401 {
			c.State = "unauthorized"
		}
		if Status(e) == 403 {
			c.State = "restricted"
		}
		c.Message = safeError(e).Error()
	}
	k.mu.Lock()
	c.RetentionWarnings = []model.Warning{}
	c.RetainedBytes = k.retainedBytesLocked()
	c.RetentionBudgetBytes = k.retentionBudget
	for key, message := range k.retentionWarnings {
		c.RetentionWarnings = append(c.RetentionWarnings, model.Warning{Kind: key, Message: message})
	}
	sort.Slice(c.RetentionWarnings, func(i, j int) bool { return c.RetentionWarnings[i].Kind < c.RetentionWarnings[j].Kind })
	if !k.lastRead.IsZero() {
		at := k.lastRead
		c.LastSuccessfulRead = &at
	}
	for key, msg := range k.failures {
		c.CollectionFailures = append(c.CollectionFailures, model.Warning{Kind: strings.SplitN(key, ":", 2)[0], Message: msg})
	}
	for _, s := range k.streams {
		v := model.StreamStatus{Kind: s.kind, Namespace: s.namespace, State: s.state, Message: s.message}
		if !s.lastSync.IsZero() {
			at := s.lastSync
			v.LastSync = &at
		}
		c.Streams = append(c.Streams, v)
	}
	k.mu.Unlock()
	k.mapMu.Lock()
	if k.discoveryNote != "" {
		c.Discovery = k.discoveryNote
	}
	k.mapMu.Unlock()
	if c.State == "connected" && len(c.CollectionFailures) > 0 {
		c.State = "degraded"
		c.Message = "Kubernetes API is reachable; some resource collections are unavailable."
	}
	sort.Slice(c.Streams, func(i, j int) bool {
		return c.Streams[i].Kind+c.Streams[i].Namespace < c.Streams[j].Kind+c.Streams[j].Namespace
	})
	sort.Slice(c.CollectionFailures, func(i, j int) bool {
		return c.CollectionFailures[i].Kind+c.CollectionFailures[i].Message < c.CollectionFailures[j].Kind+c.CollectionFailures[j].Message
	})
	return c
}
