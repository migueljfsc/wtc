package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// handleStream is the SSE live feed: every newly-stored event is pushed as an
// `data: <event json>` frame. Bearer-authed (via the route table); the portal
// consumes it with fetch (EventSource can't set the Authorization header).
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // stop nginx/proxies buffering the stream

	sub := s.store.Subscribe()
	defer s.store.Unsubscribe(sub)

	// Open the stream immediately so the client's fetch resolves.
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	// Heartbeat comments keep idle connections (and proxies) alive.
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-sub:
			if !open {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
