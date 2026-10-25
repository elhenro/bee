package llm

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// a server that 400s on tools gets one retry without them, and the model is
// remembered so later requests skip the failed round trip.
func TestOpenAICompat_RetriesWithoutToolsOn400(t *testing.T) {
	var withTools, withoutTools int32
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"tools"`) {
			atomic.AddInt32(&withTools, 1)
			http.Error(w, `{"error":"registry.ollama.ai/library/tiny does not support tools"}`, http.StatusBadRequest)
			return
		}
		atomic.AddInt32(&withoutTools, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	req := Request{Model: "tiny", Stream: true, Tools: []ToolSpec{{Name: "bash", Schema: map[string]any{"type": "object"}}}}
	for round := 1; round <= 2; round++ {
		ch, err := p.Stream(t.Context(), req)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		evs := drain(t, ch, 5*time.Second)
		if len(evs) != 2 || evs[0].Delta != "ok" {
			t.Fatalf("round %d events = %+v", round, evs)
		}
	}
	if withTools != 1 || withoutTools != 2 {
		t.Errorf("requests with tools = %d, without = %d, want 1 and 2", withTools, withoutTools)
	}
}

func TestOpenAICompat_HardStatusIsNotRetried(t *testing.T) {
	var hits int32
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "bad key", http.StatusUnauthorized)
	})
	_, err := p.Stream(t.Context(), Request{Model: "m", Stream: true})
	if err == nil || !strings.Contains(err.Error(), "status 401") || hits != 1 {
		t.Errorf("err = %v, hits = %d", err, hits)
	}
}
