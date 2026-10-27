package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doneUsage(t *testing.T, evs []Event) *Usage {
	t.Helper()
	for _, ev := range evs {
		if ev.Type == EventDone {
			return ev.Usage
		}
	}
	t.Fatal("no done event")
	return nil
}

func TestResponsesUsageCarriesCachedTokens(t *testing.T) {
	evs := responsesStream(t,
		`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":100,"output_tokens":7,"input_tokens_details":{"cached_tokens":64}}}}`)
	u := doneUsage(t, evs)
	if u == nil || u.InputTokens != 100 || u.CachedTokens != 64 {
		t.Errorf("usage = %+v, want input 100 cached 64", u)
	}
}

func TestGeminiUsageCarriesCachedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseWrite(w, map[string]any{
			"candidates":    []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "x"}}}, "finishReason": "STOP"}},
			"usageMetadata": map[string]any{"promptTokenCount": 50, "candidatesTokenCount": 2, "cachedContentTokenCount": 30},
		})
	}))
	defer srv.Close()
	ch, err := NewGemini(GeminiConfig{BaseURL: srv.URL, APIKey: "k"}).Stream(context.Background(), Request{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if u := doneUsage(t, drainEvents(ch)); u == nil || u.InputTokens != 50 || u.CachedTokens != 30 {
		t.Errorf("usage = %+v, want input 50 cached 30", u)
	}
}

// cache tallies repeated on message_delta override the message_start snapshot,
// and InputTokens keeps meaning the full prompt size.
func TestClaudeUsageMergesCacheFromMessageDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseWrite(w,
			map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 5, "output_tokens": 1}}},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"},
				"usage": map[string]any{"input_tokens": 5, "output_tokens": 9, "cache_read_input_tokens": 40, "cache_creation_input_tokens": 10}},
		)
	}))
	defer srv.Close()
	ch, err := NewClaude(ClaudeConfig{BaseURL: srv.URL}).Stream(context.Background(), Request{Model: "claude-sonnet-4-5", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	u := doneUsage(t, drainEvents(ch))
	if u == nil || u.InputTokens != 55 || u.CachedTokens != 40 || u.OutputTokens != 9 {
		t.Errorf("usage = %+v, want input 55 cached 40 output 9", u)
	}
}
