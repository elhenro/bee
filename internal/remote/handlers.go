package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := htmlEscape(s.opts.Title)
	fmt.Fprintf(w, indexHTML, title, title)
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
