package job

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/jobs"
	"github.com/elhenro/bee/internal/tools"
)

func sh(script string) jobs.Build {
	return func(ctx context.Context, out io.Writer) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "bash", "-c", script)
		cmd.Stdout, cmd.Stderr = out, out
		tools.GroupKill(cmd, time.Second)
		return cmd
	}
}

func run(t *testing.T, tl *Tool, in map[string]any) tools.Result {
	t.Helper()
	res, err := tl.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestListEmptyAndPopulated(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	tl := New(m)
	if got := run(t, tl, map[string]any{"action": "list"}).Content; got != "no background jobs" {
		t.Fatalf("empty list = %q", got)
	}
	id, _ := m.Spawn("sleep 30", 0, sh("sleep 30"))
	got := run(t, tl, map[string]any{"action": "list"}).Content
	if !strings.Contains(got, "#1 running") || !strings.Contains(got, "sleep 30") || id != 1 {
		t.Fatalf("list = %q", got)
	}
}

func TestOutputShowsPartialOutput(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	m.Spawn("tick", 0, sh("echo first; sleep 30"))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if j, _ := m.Get(1); strings.Contains(j.Output, "first") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	res := run(t, New(m), map[string]any{"action": "output", "id": float64(1)})
	if res.IsError || !strings.Contains(res.Content, "first") || !strings.Contains(res.Content, "running") {
		t.Fatalf("res = %+v", res)
	}
}

func TestKillAndErrors(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	tl := New(m)
	m.Spawn("sleep", 0, sh("sleep 30"))
	if res := run(t, tl, map[string]any{"action": "kill", "id": "#1"}); res.IsError {
		t.Fatalf("kill: %+v", res)
	}
	for name, in := range map[string]map[string]any{
		"unknown id":   {"action": "output", "id": float64(9)},
		"missing id":   {"action": "kill"},
		"bad action":   {"action": "wait"},
		"kill unknown": {"action": "kill", "id": float64(9)},
	} {
		if res := run(t, tl, in); !res.IsError {
			t.Errorf("%s: want error, got %q", name, res.Content)
		}
	}
}
