//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
	bghive "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
)

// killedProvider plays `bee bg --kill`: it removes the sidecar and cancels the
// run while a turn is in flight.
type killedProvider struct {
	id     string
	cancel context.CancelFunc
}

func (p *killedProvider) Name() string { return "stub" }
func (p *killedProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	_ = bgreg.Remove(p.id)
	p.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestKilledBgLoopDoesNotRecreateSidecar(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Memory.Enabled = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const id = "killed-session"
	eng := &loop.Engine{Provider: &killedProvider{id: id, cancel: cancel}, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: t.TempDir()}
	if err := runBgLoop(ctx, eng, id, "work"); err != nil {
		t.Fatalf("runBgLoop: %v", err)
	}
	if _, err := bgreg.Read(id); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("killed session sidecar came back: err=%v", err)
	}
}

func TestStoppedBgLoopStillRecordsDone(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	if err := bgreg.Write(bgreg.Status{SessionID: "s1", State: bgreg.StateActive}); err != nil {
		t.Fatal(err)
	}
	writeStopped(bgreg.Status{SessionID: "s1", State: bgreg.StateActive})
	if st, err := bgreg.Read("s1"); err != nil || st.State != bgreg.StateDone {
		t.Fatalf("want done sidecar, got %+v err=%v", st, err)
	}
}

func sleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	c := exec.Command("sleep", "30")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	return c
}

func writePidFile(t *testing.T, id string, pid int) string {
	t.Helper()
	dir, err := bghive.LogDir()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, id+".pid")
	if err := os.WriteFile(p, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func waitExit(c *exec.Cmd, d time.Duration) bool {
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// a finished session's pid file can outlive the process; the pid may be reused
func TestKillBgSkipsPidOfFinishedSession(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	c := sleeper(t)
	pidFile := writePidFile(t, "old", c.Process.Pid)
	_ = bgreg.Write(bgreg.Status{SessionID: "old", State: bgreg.StateDone, PID: c.Process.Pid})
	killBg("old")
	if waitExit(c, 500*time.Millisecond) {
		t.Fatal("killBg signalled a process of a finished session")
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale pid file left behind")
	}
}

func TestKillBgStopsLiveSession(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	c := sleeper(t)
	_ = bgreg.Write(bgreg.Status{SessionID: "live", State: bgreg.StateAwaiting, PID: c.Process.Pid})
	killBg("live")
	if !waitExit(c, 5*time.Second) {
		t.Fatal("live session was not interrupted")
	}
	if _, err := bgreg.Read("live"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sidecar not removed: %v", err)
	}
}
