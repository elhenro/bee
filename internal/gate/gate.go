// Package gate runs a repo's verify commands before unattended work lands:
// every command must exit 0, or the work goes back to its author with the
// failing output. Runs take the machine-wide heavy lock so parallel bees never
// stack test suites on one box.
package gate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// File is the repo-local gate: one command per line, `#` comments, an optional
// leading `$ `. It wins over the [swarm] verify fallback in config.
const File = ".bee/verify"

// outputTail caps how much failing output travels back to the author.
const outputTail = 4000

// Result is one gate pass.
type Result struct {
	OK     bool
	Failed string // the command that failed, empty when OK
	Output string // tail of the failing command's combined output
	Ran    int    // commands that ran, including a failing one
}

// Summary is the one-paragraph feedback handed to the model on a red gate.
func (r Result) Summary() string {
	if r.OK {
		return fmt.Sprintf("verify green (%d command(s))", r.Ran)
	}
	return fmt.Sprintf("verify failed: `%s`\n%s", r.Failed, r.Output)
}

// Commands returns the gate for repoRoot: its .bee/verify when present,
// otherwise fallback. Empty means no gate.
func Commands(repoRoot string, fallback []string) []string {
	f, err := os.Open(filepath.Join(repoRoot, File))
	if err != nil {
		return fallback // no repo gate is the common case
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		line = strings.TrimSpace(strings.TrimPrefix(line, "$"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Run executes cmds in dir, in order, stopping at the first failure. Each
// command gets timeout; a hung one is killed with its whole process group.
// Holds the heavy lock for the full pass.
func Run(ctx context.Context, dir string, cmds []string, timeout time.Duration) (Result, error) {
	if len(cmds) == 0 {
		return Result{OK: true}, nil
	}
	unlock, err := lockHeavy(ctx)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	var res Result
	for _, c := range cmds {
		res.Ran++
		out, err := runOne(ctx, dir, c, timeout)
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if err != nil {
			res.Failed = c
			res.Output = tail(out, err)
			return res, nil
		}
	}
	res.OK = true
	return res, nil
}

func runOne(ctx context.Context, dir, command string, timeout time.Duration) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(cctx, command)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return buf.Bytes(), fmt.Errorf("timed out after %s", timeout)
	}
	return buf.Bytes(), err
}

func tail(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if len(s) > outputTail {
		s = "…" + s[len(s)-outputTail:]
	}
	if s == "" {
		return err.Error()
	}
	return s + "\n(" + err.Error() + ")"
}

// shellCommand builds the command with process-group kill on cancel.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	setGroupKill(cmd)
	return cmd
}
