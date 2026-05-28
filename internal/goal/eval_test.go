package goal

import (
	"context"
	"errors"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// stubProvider replays a scripted text response as a single side call. err, when
// set, is emitted as an EventError before EventDone.
type stubProvider struct {
	script string
	err    error
}

func (s stubProvider) Name() string { return "stub" }

func (s stubProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 3)
	if s.script != "" {
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: s.script}
	}
	if s.err != nil {
		ch <- llm.Event{Type: llm.EventError, Err: s.err}
	}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

func sampleMsgs() []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "fix the build"},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "ran the tests, all green"},
		}},
	}
}

func TestEvaluateMet(t *testing.T) {
	p := stubProvider{script: `{"met":true,"reason":"tests pass"}`}
	v, err := Evaluate(context.Background(), p, "fast", "tests pass", sampleMsgs())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !v.Met {
		t.Fatal("want Met true")
	}
	if v.Reason != "tests pass" {
		t.Fatalf("reason = %q", v.Reason)
	}
}

func TestEvaluateNotMet(t *testing.T) {
	p := stubProvider{script: `here you go: {"met":false,"reason":"still failing"}`}
	v, err := Evaluate(context.Background(), p, "fast", "tests pass", sampleMsgs())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if v.Met {
		t.Fatal("want Met false")
	}
	if v.Reason != "still failing" {
		t.Fatalf("reason = %q", v.Reason)
	}
}

func TestEvaluateMalformed(t *testing.T) {
	p := stubProvider{script: "not json"}
	v, err := Evaluate(context.Background(), p, "fast", "tests pass", sampleMsgs())
	if err == nil {
		t.Fatal("want error on malformed response")
	}
	if v.Met {
		t.Fatal("want Met false on malformed")
	}
}

func TestEvaluateNilProvider(t *testing.T) {
	v, err := Evaluate(context.Background(), nil, "fast", "tests pass", sampleMsgs())
	if err != nil {
		t.Fatalf("nil provider should not error: %v", err)
	}
	if v.Met {
		t.Fatal("want Met false for nil provider")
	}
}

func TestEvaluateStreamError(t *testing.T) {
	p := stubProvider{err: errors.New("boom")}
	v, err := Evaluate(context.Background(), p, "fast", "tests pass", sampleMsgs())
	if err == nil {
		t.Fatal("want error when stream errors with empty buffer")
	}
	if v.Met {
		t.Fatal("want Met false")
	}
}

func TestContinuation(t *testing.T) {
	got := Continuation("ship it", "not done")
	if got == "" {
		t.Fatal("empty continuation")
	}
	for _, want := range []string{"ship it", "not done", "goal"} {
		if !contains(got, want) {
			t.Fatalf("continuation missing %q: %q", want, got)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
