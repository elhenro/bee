package loop

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

func TestLooksLikeUntaggedReasoning(t *testing.T) {
	reasoning := []string{
		"Thinking Process:\n1. Identify the question.",
		"Let me think about this step by step.",
		"The user is asking for the capital of France.",
		"Step 1: parse the input.",
	}
	for _, s := range reasoning {
		if !looksLikeUntaggedReasoning(s) {
			t.Errorf("expected reasoning detected for %q", s)
		}
	}
	answers := []string{
		"The capital of France is Paris.",
		"",
		"I updated the config and ran the tests; they pass.",
		"Here is a function that thinks about the problem.", // 'think' mid-sentence must not trip
	}
	for _, s := range answers {
		if looksLikeUntaggedReasoning(s) {
			t.Errorf("false positive on plain answer %q", s)
		}
	}
}

func TestHasThinkingBlock(t *testing.T) {
	with := types.Message{Content: []types.ContentBlock{
		{Type: types.BlockThinking, Text: "hmm"},
		{Type: types.BlockText, Text: "answer"},
	}}
	if !hasThinkingBlock(with) {
		t.Error("expected thinking block detected")
	}
	without := types.Message{Content: []types.ContentBlock{{Type: types.BlockText, Text: "answer"}}}
	if hasThinkingBlock(without) {
		t.Error("false positive: no thinking block present")
	}
}

func TestVerifyThinkingSuppression_WarnsOnce(t *testing.T) {
	warnCh := make(chan string, 4)
	cfg := config.Defaults()
	cfg.DefaultModel = "qwen3-30b-a3b"
	eng := &Engine{Cfg: cfg, WarnCh: warnCh, thinkingSuppressRequested: true}

	msg := types.Message{Content: []types.ContentBlock{{Type: types.BlockThinking, Text: "deliberating"}}}
	eng.verifyThinkingSuppression("", msg)
	eng.verifyThinkingSuppression("", msg) // second call must not warn again

	select {
	case w := <-warnCh:
		if !strings.Contains(w, "ignores thinking suppression") {
			t.Errorf("unexpected warning text: %q", w)
		}
	default:
		t.Fatal("expected a warning on WarnCh")
	}
	select {
	case w := <-warnCh:
		t.Errorf("expected only one warning, got a second: %q", w)
	default:
	}
}

func TestVerifyThinkingSuppression_SilentWhenNotRequested(t *testing.T) {
	warnCh := make(chan string, 2)
	eng := &Engine{Cfg: config.Defaults(), WarnCh: warnCh, thinkingSuppressRequested: false}
	msg := types.Message{Content: []types.ContentBlock{{Type: types.BlockThinking, Text: "x"}}}
	eng.verifyThinkingSuppression("", msg)
	select {
	case w := <-warnCh:
		t.Errorf("should not warn when suppression not requested, got %q", w)
	default:
	}
}

// emptyProvider always streams a bare Done — no text, no tool, no reasoning.
type emptyProvider struct{ calls int32 }

func (p *emptyProvider) Name() string { return "empty" }

func (p *emptyProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	atomic.AddInt32(&p.calls, 1)
	ch := make(chan llm.Event, 2)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "   "} // whitespace only
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

// a model that returns whitespace-only turns is nudged once then bails with
// EmptyCompletionError rather than ending on a blank answer or spinning.
func TestRun_EmptyCompletionBails(t *testing.T) {
	prov := &emptyProvider{}
	cfg := config.Defaults()
	cfg.Role = "worker"
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	eng := &Engine{
		SkipPostureClassifier: true,
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Stdout:   io.Discard,
		Cfg:      cfg,
		Cwd:      ".",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := eng.Run(ctx, "ping")
	if !errors.Is(err, ErrEmptyCompletion) {
		t.Fatalf("expected ErrEmptyCompletion, got %v", err)
	}
	if got := atomic.LoadInt32(&prov.calls); got != emptyCompletionBailAt {
		t.Fatalf("expected %d calls before bail, got %d", emptyCompletionBailAt, got)
	}
}
