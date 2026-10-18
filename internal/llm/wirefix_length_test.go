package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIStreamLengthStopDoesNotRepairToolArgs(t *testing.T) {
	chunk := func(finish any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "c1", "function": map[string]any{"name": "write", "arguments": `{"path":"a","content":"half`},
			}}},
			"finish_reason": finish,
		}}}
	}
	for name, finish := range map[string]any{"length": "length", "no finish": nil} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sseWrite(w, chunk(finish)) // no [DONE]
		}))
		p := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL})
		ch, err := p.Stream(context.Background(), Request{Model: "m", Stream: true})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ev := range drainEvents(ch) {
			if ev.Type == EventToolUse {
				found = true
				if _, ok := ev.ToolUse.Input["_parse_error"]; !ok {
					t.Errorf("%s: truncated call must carry _parse_error, got %v", name, ev.ToolUse.Input)
				}
			}
		}
		if !found {
			t.Errorf("%s: no tool use", name)
		}
		srv.Close()
	}
}
