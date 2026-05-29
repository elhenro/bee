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

// TestBuildTranscript_IncludesToolEvidence verifies the judge transcript
// surfaces tool calls and their results, not just prose. Without this a goal
// like "create a file" is invisible to the judge — the proof lives in the
// write tool's result, not in the agent's claim.
func TestBuildTranscript_IncludesToolEvidence(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "creating it"},
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				Name: "write", Input: map[string]any{"path": "hello.txt", "content": "hello bee"}}},
		}},
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "wrote 9 bytes to hello.txt"}},
		}},
	}
	out := buildTranscript(msgs)
	for _, want := range []string{"called write", "path=hello.txt", "ok result", "wrote 9 bytes"} {
		if !contains(out, want) {
			t.Errorf("transcript missing %q\ngot: %s", want, out)
		}
	}
}

// TestBuildTranscript_MarksErroredResult shows failed tool calls are flagged so
// the judge does not mistake an error for success.
func TestBuildTranscript_MarksErroredResult(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "permission denied", IsError: true}},
		}},
	}
	if out := buildTranscript(msgs); !contains(out, "error result") {
		t.Errorf("errored result not marked\ngot: %s", out)
	}
}

func TestParseVerdict_VerdictLine(t *testing.T) {
	cases := []struct {
		raw     string
		wantMet bool
		wantRsn string
	}{
		{"some reasoning here\nVERDICT: MET — file created with exact content", true, "file created with exact content"},
		{"VERDICT: UNMET — no evidence of the file", false, "no evidence of the file"},
		{"thinking...\nverdict: met", true, ""},
		{"The user wants X. I checked.\nVERDICT: UNMET", false, ""},
		// a stray mention earlier must not win over the final verdict line
		{"I will end with VERDICT: ... \nVERDICT: MET — done", true, "done"},
	}
	for _, c := range cases {
		v, err := parseVerdict(c.raw)
		if err != nil {
			t.Errorf("parseVerdict(%q) err: %v", c.raw, err)
			continue
		}
		if v.Met != c.wantMet {
			t.Errorf("parseVerdict(%q) met=%v want %v", c.raw, v.Met, c.wantMet)
		}
		if c.wantRsn != "" && v.Reason != c.wantRsn {
			t.Errorf("parseVerdict(%q) reason=%q want %q", c.raw, v.Reason, c.wantRsn)
		}
	}
}

func TestParseVerdict_JSONFallback(t *testing.T) {
	v, err := parseVerdict(`prose {"met": true, "reason": "ok"} trailing`)
	if err != nil || !v.Met || v.Reason != "ok" {
		t.Errorf("json fallback broken: %+v err=%v", v, err)
	}
}

func TestParseVerdict_Unparseable(t *testing.T) {
	if _, err := parseVerdict("The user wants to check if the condition is met: Condition"); err == nil {
		t.Error("expected error on reply with neither verdict line nor json")
	}
}
