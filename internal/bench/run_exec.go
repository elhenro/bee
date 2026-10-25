package bench

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/goal"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// runBee spawns the real binary against the task's goal in the sandbox and
// returns its stderr, where the goal loop reports its verdict. stdout carries
// model-authored text and is dropped: it must never decide a verdict. beeHome
// is a per-run BEE_HOME so benchmark runs never read or write the user's real
// waggles, memory or usage log.
func runBee(ctx context.Context, t Task, opt Options, sandbox, sessDir, beeHome string) (string, error) {
	args := []string{"run", "--headless", "--yes"}
	if opt.Provider != "" {
		args = append(args, "--provider", opt.Provider)
	}
	if opt.Model != "" {
		args = append(args, "--model", opt.Model)
	}
	args = append(args, "/goal "+t.Prompt)

	cctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, opt.BeeBin, args...)
	cmd.Dir = sandbox
	cmd.Env = append(os.Environ(),
		"BEE_SESSIONS_DIR="+sessDir,
		"BEE_HOME="+beeHome,
	)
	if opt.ConfigFile != "" {
		cmd.Env = append(cmd.Env, "BEE_CONFIG="+opt.ConfigFile)
	}
	cmd.Env = append(cmd.Env, opt.ExtraEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// runShell runs a task's setup line. It runs from the invocation cwd (not the
// empty sandbox) so relative fixture paths like "cp -r bench/fixtures/x $SANDBOX"
// resolve against the suite. trusted task-suite shell — see checks.go. timeout
// caps it so a hung setup can't stall the suite (0 = no cap).
func runShell(ctx context.Context, line, sandbox string, timeout time.Duration) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(line, "$SANDBOX", sandbox))
	cmd.Env = append(os.Environ(), "SANDBOX="+sandbox)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// sessionFile finds the single session jsonl bee wrote into sessDir.
func sessionFile(sessDir string) (string, error) {
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			return filepath.Join(sessDir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no session written")
}

func readTranscript(sessDir string) ([]types.Message, error) {
	path, err := sessionFile(sessDir)
	if err != nil {
		return nil, err
	}
	return session.ReadFile(path)
}

// goalAchieved reports whether the goal loop printed its achieved verdict as a
// line of its own on stderr.
func goalAchieved(stderr string) bool {
	_, ok := verdictLine(stderr, goal.AchievedPrefix)
	return ok
}

// verdictLine returns the text after prefix on the last stderr line that starts
// with it. Line-anchored so a mid-line mention (a tool echoing the phrase) does
// not count.
func verdictLine(stderr, prefix string) (string, bool) {
	rest, found := "", false
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, prefix) {
			rest, found = strings.TrimPrefix(line, prefix), true
		}
	}
	return rest, found
}

func verdictReason(stderr string, succeeded bool) string {
	if r, ok := verdictLine(stderr, goal.AchievedPrefix); ok {
		return strings.TrimSpace(r)
	}
	if r, ok := verdictLine(stderr, goal.StoppedPrefix); ok {
		return strings.TrimSpace(goal.StoppedPrefix + r)
	}
	if succeeded {
		return "checks passed"
	}
	return "checks failed"
}
