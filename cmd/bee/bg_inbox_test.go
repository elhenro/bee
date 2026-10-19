package main

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
)

// queuedReplyProvider plays a user who replies while turn one runs, and the
// agent closing that turn with a final-text sentinel.
type queuedReplyProvider struct {
	id       string
	sentinel string
	cancel   context.CancelFunc
	reqs     []llm.Request
}

func (p *queuedReplyProvider) Name() string { return "stub" }
func (p *queuedReplyProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.reqs = append(p.reqs, req)
	ch := make(chan llm.Event, 2)
	if len(p.reqs) == 1 {
		_ = bgreg.InboxAppend(p.id, "follow up please")
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: p.sentinel}
	} else {
		p.cancel()
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "ok"}
	}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func TestBgLoopRunsReplyQueuedBeforeSentinel(t *testing.T) {
	for _, sentinel := range []string{"DONE: finished", "BLOCKED: stuck"} {
		t.Run(sentinel, func(t *testing.T) {
			t.Setenv("BEE_HOME", t.TempDir())
			t.Setenv("BEE_AGENT_WORKTREE", "1")
			cfg := config.Defaults()
			cfg.Memory.Enabled = false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			const id = "queued-reply"
			prov := &queuedReplyProvider{id: id, sentinel: sentinel, cancel: cancel}
			eng := &loop.Engine{Provider: prov, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: t.TempDir()}
			if err := runBgLoop(ctx, eng, id, "work"); err != nil {
				t.Fatalf("runBgLoop: %v", err)
			}
			if len(prov.reqs) != 2 {
				t.Fatalf("want a second turn for the queued reply, got %d request(s)", len(prov.reqs))
			}
			last := prov.reqs[1].Messages[len(prov.reqs[1].Messages)-1]
			if got := last.Content[0].Text; !strings.Contains(got, "follow up please") {
				t.Fatalf("second turn did not carry the queued reply: %q", got)
			}
		})
	}
}

// scriptedProvider answers each request with the next canned text and records
// what it was asked.
type scriptedProvider struct {
	replies []string
	reqs    []llm.Request
	cancel  context.CancelFunc
}

func (p *scriptedProvider) Name() string { return "stub" }
func (p *scriptedProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.reqs = append(p.reqs, req)
	text := "ok"
	if len(p.reqs) <= len(p.replies) {
		text = p.replies[len(p.reqs)-1]
	}
	if len(p.reqs) >= len(p.replies) {
		p.cancel()
	}
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: text}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func runBgOnce(t *testing.T, id, task string, replies ...string) *scriptedProvider {
	t.Helper()
	cfg := config.Defaults()
	cfg.Memory.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prov := &scriptedProvider{replies: replies, cancel: cancel}
	eng := &loop.Engine{Provider: prov, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: t.TempDir()}
	if err := runBgLoop(ctx, eng, id, task); err != nil {
		t.Fatalf("runBgLoop: %v", err)
	}
	return prov
}

func firstUserText(req llm.Request) string {
	for _, m := range req.Messages {
		if m.Role == "user" && len(m.Content) > 0 {
			return m.Content[0].Text
		}
	}
	return ""
}

// a woken agent starts with the reply that was waiting, and a later restart
// does not replay it.
func TestBgLoopStartsWithUnreadReplyOnce(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("BEE_AGENT_WORKTREE", "1")
	const id = "woken"
	_ = bgreg.InboxAppend(id, "sent while stopped")

	first := runBgOnce(t, id, "the task", "ok")
	if got := firstUserText(first.reqs[0]); !strings.Contains(got, "the task") || !strings.Contains(got, "sent while stopped") {
		t.Fatalf("start did not carry task + waiting reply: %q", got)
	}
	again := runBgOnce(t, id, "the task", "ok")
	if got := firstUserText(again.reqs[0]); strings.Contains(got, "sent while stopped") {
		t.Fatalf("restart replayed an already-read reply: %q", got)
	}
}
