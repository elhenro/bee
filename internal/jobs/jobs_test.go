package jobs

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/tools"
)

func sh(script string) Build {
	return func(ctx context.Context, out io.Writer) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "bash", "-c", script)
		cmd.Stdout, cmd.Stderr = out, out
		tools.GroupKill(cmd, time.Second)
		return cmd
	}
}

func waitWake(t *testing.T, m *Manager) {
	t.Helper()
	select {
	case <-m.Wake():
	case <-time.After(5 * time.Second):
		t.Fatal("no wake")
	}
}

func TestSpawnExitedIsQueuedWithOutput(t *testing.T) {
	m := New()
	defer m.Close()
	id, err := m.Spawn("echo hi", 0, sh("echo hi"))
	if err != nil {
		t.Fatal(err)
	}
	waitWake(t, m)
	done := m.TakeDone()
	if len(done) != 1 || done[0].ID != id {
		t.Fatalf("done = %+v", done)
	}
	if done[0].State != Exited || done[0].Exit != 0 || !strings.Contains(done[0].Output, "hi") {
		t.Fatalf("job = %+v", done[0])
	}
	if m.Pending() != 0 || m.Active() {
		t.Fatalf("pending=%d active=%v after take", m.Pending(), m.Active())
	}
}

func TestSpawnNonZeroExit(t *testing.T) {
	m := New()
	defer m.Close()
	if _, err := m.Spawn("fail", 0, sh("echo boom; exit 3")); err != nil {
		t.Fatal(err)
	}
	waitWake(t, m)
	j := m.TakeDone()[0]
	if j.State != Exited || j.Exit != 3 {
		t.Fatalf("job = %+v", j)
	}
}

func TestKillIsNotQueued(t *testing.T) {
	m := New()
	defer m.Close()
	id, _ := m.Spawn("sleep", 0, sh("sleep 30"))
	if m.Running() != 1 {
		t.Fatalf("running = %d", m.Running())
	}
	if err := m.Kill(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.Running() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := m.Get(id)
	if j.State != Killed {
		t.Fatalf("state = %s", j.State)
	}
	if m.Pending() != 0 {
		t.Fatal("killed job was queued")
	}
	if err := m.Kill(id); err == nil {
		t.Fatal("second kill should fail")
	}
}

func TestTimeoutIsQueuedAsTimedOut(t *testing.T) {
	m := New()
	defer m.Close()
	if _, err := m.Spawn("sleep", 100*time.Millisecond, sh("sleep 30")); err != nil {
		t.Fatal(err)
	}
	waitWake(t, m)
	if j := m.TakeDone()[0]; j.State != TimedOut {
		t.Fatalf("state = %s", j.State)
	}
}

func TestStartFailureRecordsNothing(t *testing.T) {
	m := New()
	defer m.Close()
	_, err := m.Spawn("x", 0, func(ctx context.Context, out io.Writer) *exec.Cmd {
		return exec.CommandContext(ctx, "/definitely/not/a/binary")
	})
	if err == nil {
		t.Fatal("want start error")
	}
	if len(m.List()) != 0 {
		t.Fatal("failed start left a job")
	}
}

func TestCloseKillsRunning(t *testing.T) {
	m := New()
	id, _ := m.Spawn("sleep", 0, sh("sleep 30"))
	m.Close()
	j, _ := m.Get(id)
	if j.State != Killed {
		t.Fatalf("state after close = %s", j.State)
	}
	if _, err := m.Spawn("late", 0, sh("true")); err == nil {
		t.Fatal("spawn after close should fail")
	}
}

func TestListOrderedWithoutOutput(t *testing.T) {
	m := New()
	defer m.Close()
	a, _ := m.Spawn("a", 0, sh("echo a"))
	b, _ := m.Spawn("b", 0, sh("echo b"))
	l := m.List()
	if len(l) != 2 || l[0].ID != a || l[1].ID != b {
		t.Fatalf("list = %+v", l)
	}
	if l[0].Output != "" {
		t.Fatal("List must not carry output")
	}
}
