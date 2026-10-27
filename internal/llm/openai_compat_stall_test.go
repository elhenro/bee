package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/types"
)

// TestOpenAICompat_StallTimeout: server opens an SSE response then never
// sends a chunk. With a short stall window the provider must surface a
// "stream stalled" error before the test deadline.
func TestOpenAICompat_StallTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// hold the connection open without writing anything
		<-release
	}))
	defer srv.Close()
	defer close(release)
	t.Setenv("STALL_KEY", "sk")
	t.Setenv("BEE_STREAM_STALL_SECONDS", "1")

	p := NewOpenAICompat(OpenAICompatConfig{
		Name:    "stall",
		BaseURL: srv.URL,
		EnvKey:  "STALL_KEY",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, Request{
		Model:    "test",
		Stream:   true,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Stream returned err: %v", err)
	}

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed without sse-stall error")
			}
			if ev.Type == EventError && ev.Err != nil {
				msg := ev.Err.Error()
				// either path indicates the stall watchdog fired and torn down
				// the connection. "stream stalled" wins the race when select hits
				// <-stalled first; "use of closed network" wins when the body
				// close lands before the select.
				if !strings.Contains(msg, "stream stalled") && !strings.Contains(msg, "use of closed network") {
					t.Fatalf("expected sse stall, got %v", ev.Err)
				}
				return
			}
		case <-deadline.C:
			t.Fatalf("watchdog did not fire within 3s (timeout was 1s)")
		}
	}
}
