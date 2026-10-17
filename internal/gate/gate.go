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

	"github.com/elhenro/bee/internal/sandbox"
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

// Commands returns the gate for repoRoot: its .bee/verify when it names at
// least one command, otherwise fallback. Empty means no gate.
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
	// an emptied file must not switch the gate off
	if len(out) == 0 {
		return fallback
	}
	return out
}

// Opts tunes one gate pass. Scope is the sandbox the commands run under and
// should match the author's: verify executes code the author wrote (tests,
// build scripts), so an unconfined gate would let a confined bee escape.
type Opts struct {
	Timeout time.Duration
	Scope   sandbox.Scope
}

// Run executes cmds in dir, in order, stopping at the first failure. Each
// command gets o.Timeout; a hung one is killed with its whole process group.
// Holds the heavy lock for the full pass.
func Run(ctx context.Context, dir string, cmds []string, o Opts) (Result, error) {
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
		out, err := runOne(ctx, dir, c, o)
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

func runOne(ctx context.Context, dir, command string, o Opts) ([]byte, error) {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 600 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(cctx, dir, command, o.Scope)
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

// shellCommand builds the sandboxed command with process-group kill on
// cancel. A missing sandbox helper degrades to the bare command, as it does
// for the agent's own shell (sandbox is hardening, see internal/sandbox).
func shellCommand(ctx context.Context, dir, command string, scope sandbox.Scope) *exec.Cmd {
	argv := []string{"sh", "-c", command}
	if scope != "" && scope != sandbox.DangerFullAccess {
		argv, _ = sandbox.Wrap(sandbox.Policy{Scope: scope, Cwd: dir}, argv)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	setGroupKill(cmd)
	return cmd
}
