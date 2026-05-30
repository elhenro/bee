package ask_user

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/ask"
)

// stub asker returns a fixed answer and records the question it saw.
type stub struct {
	ans  ask.Answer
	seen ask.Question
}

func (s *stub) Ask(_ context.Context, q ask.Question) (ask.Answer, error) {
	s.seen = q
	return s.ans, nil
}

func opts() []any {
	return []any{
		map[string]any{"label": "Three.js", "recommended": true},
		map[string]any{"label": "Babylon.js"},
	}
}

func TestRun_SelectedOption(t *testing.T) {
	s := &stub{ans: ask.Answer{Index: 1, Text: "Babylon.js"}}
	tool := New(s)
	res, err := tool.Run(context.Background(), map[string]any{
		"question": "3D engine?",
		"header":   "engine",
		"options":  opts(),
	})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "Babylon.js") {
		t.Fatalf("expected selected label in content, got %q", res.Content)
	}
	if s.seen.Prompt != "3D engine?" || len(s.seen.Options) != 2 || !s.seen.Options[0].Recommended {
		t.Fatalf("question not parsed correctly: %+v", s.seen)
	}
}

func TestRun_CustomAnswer(t *testing.T) {
	s := &stub{ans: ask.Answer{Index: -1, Text: "raw WebGPU"}}
	res, _ := New(s).Run(context.Background(), map[string]any{
		"question": "engine?", "options": opts(),
	})
	if !strings.Contains(res.Content, "custom") || !strings.Contains(res.Content, "raw WebGPU") {
		t.Fatalf("expected custom answer, got %q", res.Content)
	}
}

func TestRun_Dismissed(t *testing.T) {
	s := &stub{ans: ask.Answer{Index: -1, Dismissed: true}}
	res, _ := New(s).Run(context.Background(), map[string]any{
		"question": "engine?", "options": opts(),
	})
	if !strings.Contains(res.Content, "dismissed") {
		t.Fatalf("expected dismissal note, got %q", res.Content)
	}
}

func TestRun_MissingOptionsIsError(t *testing.T) {
	res, _ := New(nil).Run(context.Background(), map[string]any{"question": "x"})
	if !res.IsError {
		t.Fatalf("expected error result for missing options, got %+v", res)
	}
}

func TestNew_NilAskerUsesStatic(t *testing.T) {
	// nil asker must auto-resolve (Static), never panic or block.
	res, err := New(nil).Run(context.Background(), map[string]any{
		"question": "engine?", "options": opts(),
	})
	if err != nil || res.IsError {
		t.Fatalf("nil asker should auto-resolve: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "Three.js") { // recommended pick
		t.Fatalf("expected recommended auto-pick, got %q", res.Content)
	}
}

func TestSpec_Name(t *testing.T) {
	if New(nil).Spec().Name != "ask_user" {
		t.Fatal("spec name must be ask_user")
	}
}
