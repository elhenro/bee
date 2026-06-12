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
	cfg.Role = "worker"
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

// TestStreamOnce_NoReplayAfterContent: once a delta is emitted, a mid-stream
// transient error must NOT replay the same request (that would duplicate the
// streamed tokens). Instead it salvages the partial turn and continues. A
// provider that drops on every call therefore bails with TruncatedStreamError
// after truncCutBailAt no-progress drops rather than looping forever.
func TestStreamOnce_NoReplayAfterContent(t *testing.T) {
	prev := preContentRetryDelay
	preContentRetryDelay = 5 * time.Millisecond
	defer func() { preContentRetryDelay = prev }()

	prov := &midStreamErrProvider{}
	cfg := config.Defaults()
	cfg.Role = "worker"
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
	_, err := eng.Run(ctx, "ping")
	if !errors.Is(err, ErrTruncatedStream) {
		t.Fatalf("expected ErrTruncatedStream after persistent mid-stream drops, got %v", err)
	}
	if got := atomic.LoadInt32(&prov.calls); got != truncCutBailAt {
		t.Fatalf("expected %d re-streams before bail, got calls=%d", truncCutBailAt, got)
	}
}

// TestStreamOnce_RecoversAfterMidStreamDrop: a transient drop after content is
// recovered — bee keeps the partial turn, nudges, and the next stream completes
// the answer. This is the recover-and-continue behavior the user asked for.
func TestStreamOnce_RecoversAfterMidStreamDrop(t *testing.T) {
	prev := preContentRetryDelay
	preContentRetryDelay = 5 * time.Millisecond
	defer func() { preContentRetryDelay = prev }()

	prov := &dropThenSucceedProvider{}
	cfg := config.Defaults()
	cfg.Role = "worker"
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
	res, err := eng.Run(ctx, "ping")
	if err != nil {
		t.Fatalf("Run should recover from a single mid-stream drop, got %v", err)
	}
	if !strings.Contains(res.FinalText, "the answer") {
		t.Fatalf("expected the post-recovery answer, got %q", res.FinalText)
	}
	if got := atomic.LoadInt32(&prov.calls); got != 2 {
		t.Fatalf("expected drop then success (2 calls), got %d", got)
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
		ch <- llm.Event{Type: llm.EventError, Err: fmt.Errorf("sse scan: %w", errors.New("use of closed network connection"))}
	}()
	return ch, nil
}

// dropThenSucceedProvider drops mid-output on the first call, then completes
// cleanly — models a flaky local server that recovers on reconnect.
type dropThenSucceedProvider struct{ calls int32 }

func (p *dropThenSucceedProvider) Name() string { return "drop-then-ok" }
func (p *dropThenSucceedProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	n := atomic.AddInt32(&p.calls, 1)
	ch := make(chan llm.Event, 3)
	go func() {
		defer close(ch)
		if n == 1 {
			ch <- llm.Event{Type: llm.EventTextDelta, Delta: "I'll start, "}
			ch <- llm.Event{Type: llm.EventError, Err: fmt.Errorf("sse scan: %w", errors.New("connection reset"))}
			return
		}
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "the answer is 42."}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}
