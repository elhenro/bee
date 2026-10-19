package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// the loop retries stream errors whose text carries a transient marker
// ("sse scan", "stream stalled"); an in-band overload must carry one.
func streamErrFor(t *testing.T, errType string) error {
	t.Helper()
	t.Setenv("CLAUDE_INBAND_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n" +
			"data: {\"type\":\"error\",\"error\":{\"type\":\"" + errType + "\",\"message\":\"boom\"}}\n\n"))
	}))
	defer srv.Close()
	p := NewClaude(ClaudeConfig{Name: "c", BaseURL: srv.URL, EnvKey: "CLAUDE_INBAND_KEY"})
	ch, err := p.Stream(context.Background(), Request{Model: "m", Stream: true,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "x"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var got error
	for ev := range ch {
		if ev.Type == EventError {
			got = ev.Err
		}
	}
	if got == nil {
		t.Fatal("no error event")
	}
	return got
}

func TestClaude_InBandOverloadIsTransient(t *testing.T) {
	for _, typ := range []string{"overloaded_error", "api_error"} {
		if err := streamErrFor(t, typ); !strings.Contains(err.Error(), "sse scan") {
			t.Errorf("%s: want transient marker, got %v", typ, err)
		}
	}
	if err := streamErrFor(t, "invalid_request_error"); strings.Contains(err.Error(), "sse scan") {
		t.Errorf("request rejection must stay fatal: %v", err)
	}
}
