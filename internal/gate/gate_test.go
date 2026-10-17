package gate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// unixOnly skips tests that need sh and flock.
func unixOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh and flock")
	}
	t.Setenv("BEE_HOME", t.TempDir())
}

func TestCommandsRepoFileWinsOverFallback(t *testing.T) {
	dir := t.TempDir()
	if got := Commands(dir, []string{"fallback"}); len(got) != 1 || got[0] != "fallback" {
		t.Fatalf("no repo file: got %v, want fallback", got)
	}
	_ = os.MkdirAll(filepath.Join(dir, ".bee"), 0o755)
	body := "# gate\n$ go build ./...\n\n  go test ./...  \n"
	if err := os.WriteFile(filepath.Join(dir, File), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Commands(dir, []string{"fallback"})
	if strings.Join(got, "|") != "go build ./...|go test ./..." {
		t.Fatalf("got %q", got)
	}
}

func TestRunStopsAtFirstFailure(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	res, err := Run(context.Background(), dir, []string{"true", "echo boom; exit 3", "touch ran"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Ran != 2 || res.Failed != "echo boom; exit 3" || !strings.Contains(res.Output, "boom") {
		t.Fatalf("res=%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); err == nil {
		t.Fatal("commands after a failure must not run")
	}
	if !strings.Contains(res.Summary(), "verify failed") {
		t.Fatalf("summary %q", res.Summary())
	}
}

func TestRunGreenAndEmpty(t *testing.T) {
	unixOnly(t)
	if res, _ := Run(context.Background(), t.TempDir(), nil, time.Minute); !res.OK {
		t.Fatal("no commands is a green gate")
	}
	res, err := Run(context.Background(), t.TempDir(), []string{"true", "true"}, time.Minute)
	if err != nil || !res.OK || res.Ran != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRunTimeoutKillsGroup(t *testing.T) {
	unixOnly(t)
	start := time.Now()
	// the child sleep must die with the shell or Wait blocks on the pipe
	res, err := Run(context.Background(), t.TempDir(), []string{"sleep 30 & sleep 30"}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Output, "timed out") {
		t.Fatalf("res=%+v", res)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout did not kill the process group")
	}
}

func TestHeavyLockSerializes(t *testing.T) {
	unixOnly(t)
	var inside, maxInside int32
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := lockHeavy(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&inside, 1)
			for {
				m := atomic.LoadInt32(&maxInside)
				if n <= m || atomic.CompareAndSwapInt32(&maxInside, m, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&inside, -1)
			unlock()
		}()
	}
	wg.Wait()
	if maxInside != 1 {
		t.Fatalf("heavy lock let %d holders in at once", maxInside)
	}
}
