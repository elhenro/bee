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

	"github.com/elhenro/bee/internal/sandbox"
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
	res, err := Run(context.Background(), dir, []string{"true", "echo boom; exit 3", "touch ran"}, Opts{Timeout: time.Minute})
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
	if res, _ := Run(context.Background(), t.TempDir(), nil, Opts{Timeout: time.Minute}); !res.OK {
		t.Fatal("no commands is a green gate")
	}
	res, err := Run(context.Background(), t.TempDir(), []string{"true", "true"}, Opts{Timeout: time.Minute})
	if err != nil || !res.OK || res.Ran != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRunTimeoutKillsGroup(t *testing.T) {
	unixOnly(t)
	start := time.Now()
	// the child sleep must die with the shell or Wait blocks on the pipe
	res, err := Run(context.Background(), t.TempDir(), []string{"sleep 30 & sleep 30"}, Opts{Timeout: 200 * time.Millisecond})
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

func TestCommandsEmptyFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".bee"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, File), []byte("# all gone\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Commands(dir, []string{"go test ./..."}); len(got) != 1 {
		t.Fatalf("an emptied gate file must not disable the gate, got %v", got)
	}
}

// verify runs author code, so it must stay inside the author's sandbox.
func TestRunConfinedScopeBlocksOutsideWrites(t *testing.T) {
	unixOnly(t)
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec probe; linux needs bwrap")
	}
	outside := filepath.Join(os.Getenv("HOME"), ".bee-gate-escape-probe")
	_ = os.Remove(outside)
	t.Cleanup(func() { _ = os.Remove(outside) })
	res, err := Run(context.Background(), t.TempDir(), []string{"touch " + outside}, Opts{Timeout: time.Minute, Scope: sandbox.WorkspaceWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(outside); serr == nil || res.OK {
		t.Fatalf("confined gate wrote outside the workspace (res=%+v)", res)
	}
}

func TestCommandsStripsOnlyDollarSpace(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".bee"), 0o755)
	body := "$ go vet ./...\n$GOBIN/lint --fast\n$HOME/bin/check\n"
	if err := os.WriteFile(filepath.Join(dir, File), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Commands(dir, nil)
	if strings.Join(got, "|") != "go vet ./...|$GOBIN/lint --fast|$HOME/bin/check" {
		t.Fatalf("got %q", got)
	}
}

func TestRunKeepsOnlyOutputTail(t *testing.T) {
	unixOnly(t)
	// ~3 MB, far past the capture cap, ending in a marker
	cmd := `i=0; while [ $i -lt 30000 ]; do echo "line $i xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"; i=$((i+1)); done; echo THE-END; exit 1`
	res, err := Run(context.Background(), t.TempDir(), []string{cmd}, Opts{Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Output, "THE-END") {
		t.Fatalf("tail lost: ok=%v out=%q", res.OK, res.Output[max(0, len(res.Output)-80):])
	}
	if len(res.Output) > outputTail+100 {
		t.Fatalf("output %d bytes, want about %d", len(res.Output), outputTail)
	}
}

func TestTailBufferBounded(t *testing.T) {
	b := &tailBuffer{max: 10}
	for i := 0; i < 100; i++ {
		_, _ = b.Write([]byte("0123456789"))
		if len(b.b) > 30 {
			t.Fatalf("buffer grew to %d", len(b.b))
		}
	}
	_, _ = b.Write([]byte("abc"))
	if got := string(b.Bytes()); got != "3456789abc" {
		t.Fatalf("tail = %q", got)
	}
}

func TestRunRefusesWhenScopeCannotBeApplied(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	res, err := Run(context.Background(), dir, []string{"touch ran"}, Opts{Timeout: time.Minute, Scope: "bogus-scope"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("an unappliable scope must fail the gate, not run unconfined")
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); err == nil {
		t.Fatal("command ran despite the unknown scope")
	}
}
