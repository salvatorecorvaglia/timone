package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"timone/internal/model"
)

func (a *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "Streaming unsupported")
		return
	}
	ch, unsubscribe, ok := a.broker.Subscribe()
	if !ok {
		fail(w, 503, "Stream capacity reached")
		return
	}
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	lastRevision := a.broker.revision.Load()
	send := func(event string, full bool) bool {
		identity, err := a.authn.Authenticate(r)
		if err != nil || identity.Name == "" {
			return false
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
		payload := model.Invalidation{Revision: lastRevision}
		if event == "invalidate" {
			payload = a.broker.Since(lastRevision)
			payload.Full = payload.Full || full
			lastRevision = payload.Revision
		}
		data, _ := json.Marshal(payload)
		_, e := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", payload.Revision, event, data)
		flusher.Flush()
		return e == nil
	}
	if !send("invalidate", true) {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !send("invalidate", false) {
				return
			}
		case <-heartbeat.C:
			if !send("heartbeat", false) {
				return
			}
		}
	}
}
