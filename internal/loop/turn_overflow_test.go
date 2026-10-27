package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// overflowProvider rejects any main request still carrying the long history as
// a context overflow, answers the summarizer, and accepts a compacted request.
type overflowProvider struct {
	mainCalls, sumCalls atomic.Int32
	failSummary         bool
	err                 string
}

func (p *overflowProvider) Name() string { return "overflow" }

func (p *overflowProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	if strings.HasPrefix(req.System, "You compress conversation history") {
		p.sumCalls.Add(1)
		if p.failSummary {
			return nil, errors.New("summarizer down")
		}
		return textStream("short summary"), nil
	}
	p.mainCalls.Add(1)
	if !carriesSummary(req.Messages) {
		err := p.err
		if err == "" {
			err = "provider anthropic status 400: prompt is too long: 215000 tokens > 200000 maximum"
		}
		return nil, errors.New(err)
	}
	return textStream("recovered"), nil
}

func textStream(text string) <-chan llm.Event {
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: text}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch
}

func overflowEngine(p llm.Provider, history int) *Engine {
	eng, _ := newEngine(p, tools.NewRegistry())
	eng.Cfg.DefaultModel = "deepseek-reasoner" // known window, far above this history
	for i := range history {
		role := types.RoleUser
		if i%2 == 1 {
			role = types.RoleAssistant
		}
		eng.InitialMessages = append(eng.InitialMessages, mkMsg(role, strings.Repeat("old turn ", 60)))
	}
	return eng
}

// an overflow rejection compacts the history and retries once, instead of
// being read as a malformed tool call.
func TestRunRecoversFromContextOverflow(t *testing.T) {
	p := &overflowProvider{}
	eng := overflowEngine(p, 8)
	res, err := eng.Run(context.Background(), "go on")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FinalText != "recovered" {
		t.Errorf("FinalText = %q, want recovered", res.FinalText)
	}
	if p.mainCalls.Load() != 2 || p.sumCalls.Load() != 1 {
		t.Errorf("main calls = %d, summary calls = %d, want 2 and 1", p.mainCalls.Load(), p.sumCalls.Load())
	}
}

// nothing to shrink: the raw error surfaces after the single request, not as a
// provider-reject wedge.
func TestOverflowSurfacesRawWhenHistoryCannotShrink(t *testing.T) {
	p := &overflowProvider{}
	eng := overflowEngine(p, 0)
	_, err := eng.Run(context.Background(), "go on")
	if err == nil || !strings.Contains(err.Error(), "prompt is too long") {
		t.Fatalf("err = %v, want the raw overflow error", err)
	}
	if errors.Is(err, ErrProviderReject) {
		t.Error("overflow must not classify as ErrProviderReject")
	}
	if p.mainCalls.Load() != 1 || p.sumCalls.Load() != 0 {
		t.Errorf("main calls = %d, summary calls = %d, want 1 and 0", p.mainCalls.Load(), p.sumCalls.Load())
	}
}

// a summarizer that is down also leaves the raw error, with one summary try.
func TestOverflowSurfacesRawWhenSummaryFails(t *testing.T) {
	p := &overflowProvider{failSummary: true}
	eng := overflowEngine(p, 8)
	_, err := eng.Run(context.Background(), "go on")
	if err == nil || !strings.Contains(err.Error(), "prompt is too long") {
		t.Fatalf("err = %v, want the raw overflow error", err)
	}
	if p.mainCalls.Load() != 1 || p.sumCalls.Load() != 1 {
		t.Errorf("main calls = %d, summary calls = %d, want 1 and 1", p.mainCalls.Load(), p.sumCalls.Load())
	}
}

// a still-overflowing retry ends the Run instead of compacting in a loop.
func TestOverflowRetriesOnce(t *testing.T) {
	p := &overflowProvider{err: "provider openai status 400: context_length_exceeded"}
	eng := overflowEngine(&alwaysOverflow{p}, 8)
	_, err := eng.Run(context.Background(), "go on")
	if err == nil || !strings.Contains(err.Error(), "context_length_exceeded") {
		t.Fatalf("err = %v, want the raw overflow error", err)
	}
	if got := p.sumCalls.Load(); got != 1 {
		t.Errorf("summary calls = %d, want 1", got)
	}
}

// alwaysOverflow rejects every main request, summarized or not.
type alwaysOverflow struct{ *overflowProvider }

func (p *alwaysOverflow) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	if !strings.HasPrefix(req.System, "You compress conversation history") {
		p.mainCalls.Add(1)
		return nil, fmt.Errorf("%s", p.err)
	}
	return p.overflowProvider.Stream(ctx, req)
}
