package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// flakyProvider fails the first N stream calls with an SSE-scan-style error
// before emitting any content, then succeeds. Exercises pre-content retry.
type flakyProvider struct {
	failures int32 // atomic — remaining failures before success
	calls    int32 // atomic — total Stream invocations
}

func (p *flakyProvider) Name() string { return "flaky" }

func (p *flakyProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	atomic.AddInt32(&p.calls, 1)
	ch := make(chan llm.Event, 2)
	if atomic.LoadInt32(&p.failures) > 0 {
		atomic.AddInt32(&p.failures, -1)
		go func() {
			defer close(ch)
			ch <- llm.Event{Type: llm.EventError, Err: fmt.Errorf("sse scan: %w", errors.New("context deadline exceeded"))}
		}()
		return ch, nil
	}
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "ok"}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

// TestStreamOnce_RetriesPreContentError: first Stream pass errors before any
// delta, loop reopens and final text is rendered + warning on WarnCh.
func TestStreamOnce_RetriesPreContentError(t *testing.T) {
	prev := preContentRetryDelay
	preContentRetryDelay = 5 * time.Millisecond
	defer func() { preContentRetryDelay = prev }()

	prov := &flakyProvider{failures: 1}
	warnCh := make(chan string, 4)
	cfg := config.Defaults()
	cfg.Mode = "edit"
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	eng := &Engine{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Stdout:   io.Discard,
		WarnCh:   warnCh,
		Cfg:      cfg,
		Cwd:      ".",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := eng.Run(ctx, "ping")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.FinalText, "ok") {
		t.Fatalf("expected text 'ok' after retry, got %q", res.FinalText)
	}
	if got := atomic.LoadInt32(&prov.calls); got != 2 {
		t.Fatalf("provider should be called twice (fail then succeed), got %d", got)
	}
	select {
	case w := <-warnCh:
		if !strings.Contains(w, "retrying") {
			t.Errorf("warning should mention retry, got %q", w)
		}
	default:
		t.Errorf("expected a warning on WarnCh, got none")
	}
}

// TestStreamOnce_NoRetryAfterContent: once a delta is emitted, mid-stream
// errors must surface as-is (replaying would duplicate tokens on screen).
func TestStreamOnce_NoRetryAfterContent(t *testing.T) {
	prev := preContentRetryDelay
	preContentRetryDelay = 5 * time.Millisecond
	defer func() { preContentRetryDelay = prev }()

	prov := &midStreamErrProvider{}
	cfg := config.Defaults()
	cfg.Mode = "edit"
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	eng := &Engine{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Stdout:   io.Discard,
		Cfg:      cfg,
		Cwd:      ".",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := eng.Run(ctx, "ping"); err == nil {
		t.Fatalf("expected error after mid-stream failure")
	}
	if got := atomic.LoadInt32(&prov.calls); got != 1 {
		t.Fatalf("provider must NOT retry once content emitted, got calls=%d", got)
	}
}

type midStreamErrProvider struct{ calls int32 }

func (p *midStreamErrProvider) Name() string { return "mid-err" }
func (p *midStreamErrProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	atomic.AddInt32(&p.calls, 1)
	ch := make(chan llm.Event, 2)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "partial"}
		ch <- llm.Event{Type: llm.EventError, Err: fmt.Errorf("sse scan: %w", errors.New("EOF"))}
	}()
	return ch, nil
}
