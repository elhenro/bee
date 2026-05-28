package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/"+s.token+"/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := htmlEscape(s.opts.Title)
	fmt.Fprintf(w, indexHTML, title, title)
}

// validOrigin returns true when the Origin header is absent (non-browser client)
// or matches the request's own host. Rejects DNS-rebinding and cross-origin
// form submissions while allowing curl and native HTTP clients.
func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// reject text/plain form CSRF (application/x-www-form-urlencoded / multipart
	// cannot set Content-Type to application/json from a cross-origin form).
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	// reject cross-origin browser requests (DNS rebinding defence).
	if !s.validOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.Text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		http.Error(w, "busy", http.StatusConflict)
		return
	}
	s.busy = true
	user := Msg{Role: "user", Text: body.Text}
	s.history = append(s.history, user)
	s.mu.Unlock()

	s.logf("recv %q", body.Text)
	s.broadcast(sseEvent{typ: "message", data: user})
	s.broadcast(sseEvent{typ: "busy", data: true})

	final, err := s.eng.Send(r.Context(), body.Text, func(chunk string) {
		if chunk != "" {
			s.broadcast(sseEvent{typ: "delta", data: chunk})
		}
	})
	if err != nil {
		final = "error: " + err.Error()
		s.logf("error %v", err)
	}

	asst := Msg{Role: "assistant", Text: final}
	s.mu.Lock()
	s.history = append(s.history, asst)
	s.busy = false
	s.mu.Unlock()

	s.broadcast(sseEvent{typ: "message", data: asst})
	s.broadcast(sseEvent{typ: "done", data: nil})
	s.broadcast(sseEvent{typ: "busy", data: false})
	s.logf("done %d chars", len(final))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(asst)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, id, replay := s.subscribe()
	defer s.unsubscribe(id)

	for _, m := range replay {
		writeSSE(w, "message", m)
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			writeSSE(w, ev.typ, ev.data)
			flusher.Flush()
		}
	}
}
