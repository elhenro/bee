package tui

import (
	"context"
	"sync"
	"testing"

	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

type tailSink struct {
	tail   func() string
	called bool
}

func (s *tailSink) Run(context.Context, string) (loop.RunResult, error) { return loop.RunResult{}, nil }
func (s *tailSink) SetPromptTail(f func() string)                       { s.tail = f }
func (s *tailSink) DeathReport(context.Context, []types.Message, error) (string, error) {
	s.called = true
	return "brief", nil
}

func TestLockedRunner_ForwardsHiveSeams(t *testing.T) {
	sink := &tailSink{}
	var lr hive.Runner = lockedRunner{r: sink, mu: &sync.Mutex{}}
	tailed, ok := lr.(hive.Tailed)
	if !ok {
		t.Fatal("lockedRunner must satisfy hive.Tailed")
	}
	tailed.SetPromptTail(func() string { return "card" })
	if sink.tail == nil || sink.tail() != "card" {
		t.Error("tail not forwarded to the wrapped runner")
	}
	rep, ok := lr.(hive.Reporter)
	if !ok {
		t.Fatal("lockedRunner must satisfy hive.Reporter")
	}
	if out, err := rep.DeathReport(context.Background(), nil, nil); err != nil || out != "brief" || !sink.called {
		t.Errorf("DeathReport not forwarded: %q %v", out, err)
	}
	// a plain runner underneath: no panic, explicit error.
	plain := lockedRunner{r: plainRunner{}, mu: &sync.Mutex{}}
	plain.SetPromptTail(func() string { return "" })
	if _, err := plain.DeathReport(context.Background(), nil, nil); err == nil {
		t.Error("expected an error from a runner that cannot report")
	}
}

type plainRunner struct{}

func (plainRunner) Run(context.Context, string) (loop.RunResult, error) { return loop.RunResult{}, nil }
