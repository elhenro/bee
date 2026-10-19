// Package shell implements the shell tool: bash -c execution with timeout
// and output truncation. No sandboxing here — slice 1F adds that layer.
//
// non-login non-interactive bash skips ~/.bash_profile and ~/.bashrc to
// avoid tripping over user rc files that misbehave under sandbox.
package shell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
)

const (
	toolName       = "bash"
	defaultTimeout = 120 * time.Second

	// capture window per run: first and last bytes kept, middle dropped. the
	// loop owns the per-profile cap (and the spill file) on top of this; this
	// only bounds memory for runaway output.
	captureHead = 256 << 10
	captureTail = 256 << 10
)

// Options configures shell behavior beyond approval gating.
//
// UseUserRC sources the user's interactive rc file (.zshrc / .bashrc) before
// each command so aliases and shell functions are available. Default false
// preserves the hermetic shape. Shell and RCFile override autodetection
// (autodetect = $SHELL → ~/.zshrc or ~/.bashrc).
//
// Allowed, when non-nil, enables untrusted-mode gating: a command it rejects
// is put to the approver (if any) before running, even when no dangerous
// pattern matched. nil leaves only the dangerous-pattern gate.
type Options struct {
	UseUserRC bool
	Shell     string
	RCFile    string
	Allowed   func(cmd string) bool
}

// Tool is the shell executor.
//
// approver, when non-nil, is consulted whenever safety.DetectDangerous flags
// the command. nil approver = no gating (legacy behavior). Hardline checks in
// safety.CheckShellCommand always run regardless of approver.
type Tool struct {
	approver approval.Approver
	opts     Options
}

// New returns a shell tool with no approval gating. Use NewWithApprover to
// enable the dangerous-pattern prompt flow.
func New() tools.Tool { return &Tool{} }

// NewWithApprover returns a shell tool that consults app before running any
// command flagged by safety.DetectDangerous. A Deny verdict aborts execution
// and returns an explanatory IsError result.
func NewWithApprover(app approval.Approver) tools.Tool { return &Tool{approver: app} }

// NewWithOptions wires both approval gating and shell options. nil app =
// no approval gating.
func NewWithOptions(app approval.Approver, opts Options) tools.Tool {
	return &Tool{approver: app, opts: opts}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "Run a shell command via `bash -c`. Combined stdout+stderr returned, capped at 20 KB. Already runs in bee's cwd — do NOT prepend `cd <dir> &&`; use the cwd field only to override.",
		PromptSnippet: "Execute bash.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "Command line passed to bash -c. Do not prepend `cd <dir> &&` — process cwd is already set.",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Hard timeout in seconds (default 120). Raise it for long builds or test suites.",
				},
				"cwd": map[string]any{
					"type":        "string",
					"description": "Override working directory. Omit to inherit bee's cwd (the usual case).",
				},
			},
			"required": []string{"command"},
		},
	}
}

// Run executes the command and returns the result.
func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	cmdStr, ok := input["command"].(string)
	if !ok || strings.TrimSpace(cmdStr) == "" {
		return tools.Result{Content: "missing or empty 'command' field", IsError: true}, nil
	}
	// auto-strip redundant `cd <cwd> &&` prefix. Tiny models (qwen3, gemma3)
	// habitually prepend `cd /abs/path && ...` even when the process is
	// already there — wastes tokens and clutters transcripts. Stripping
	// silently is cleaner than re-nagging through the system prompt. If the
	// target is a different dir, leave the command alone (legitimate intent).
	// an explicit cwd override means the cd is not redundant: the command runs
	// there, so dropping the cd would change where it executes.
	var stripNote string
	if override, _ := input["cwd"].(string); override == "" {
		cmdStr, stripNote = stripRedundantCd(cmdStr)
	}
	// display command = unwrapped form when engine pre-wrapped with sandbox-exec;
	// otherwise the modal would show the helper profile, not the user's intent.
	displayCmd := cmdStr
	if v, ok := input["_orig_command"].(string); ok && v != "" {
		displayCmd = v
	}
	if err := safety.CheckShellCommand(displayCmd); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	// defense in depth: also vet the command that will actually execute, so a
	// display/exec mismatch (a spoofed _orig_command slipping past input
	// sanitization, or a wrapper bug) can never downgrade the hardline check.
	if cmdStr != displayCmd {
		if err := safety.CheckShellCommand(cmdStr); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	if t.approver != nil {
		hits := safety.DetectAllDangerous(displayCmd)
		if cmdStr != displayCmd {
			hits = mergeDangers(hits, safety.DetectAllDangerous(cmdStr))
		}
		if len(hits) == 0 && t.opts.Allowed != nil && !t.opts.Allowed(displayCmd) {
			hits = []safety.Danger{{Key: untrustedKey(displayCmd), Desc: "command is not on the known-safe list (approval = untrusted)"}}
		}
		// each matched pattern needs its own grant: approving rm-recursive must
		// not carry a chained `curl | sh` through.
		for _, h := range hits {
			d, err := t.approver.Request(ctx, displayCmd, h.Key, h.Desc)
			if err != nil {
				return tools.Result{Content: fmt.Sprintf("approval error: %v", err), IsError: true}, nil
			}
			if d == approval.Deny {
				return tools.Result{
					Content: fmt.Sprintf("refused by user: %s (%s). Try a different approach.", h.Desc, h.Key),
					IsError: true,
				}, nil
			}
		}
	}

	timeout := defaultTimeout
	if v, ok := input["timeout_seconds"]; ok {
		secs, err := toInt(v)
		if err != nil {
			return tools.Result{Content: fmt.Sprintf("bad timeout_seconds: %v", err), IsError: true}, nil
		}
		if secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}

	cwd := ""
	if v, ok := input["cwd"].(string); ok {
		cwd = v
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shellBin, finalCmd := t.buildInvocation(cmdStr)
	cmd := exec.CommandContext(runCtx, shellBin, "-c", finalCmd)
	cmd.Dir = cwd
	buf := tools.NewCapBuffer(captureHead, captureTail)
	cmd.Stdout = buf
	cmd.Stderr = buf
	// own process group so timeout/cancel kills backgrounded children too,
	// not just the bash leader. WaitDelay caps how long we wait for pipes a
	// surviving child still holds.
	tools.GroupKill(cmd, 2*time.Second)

	err := cmd.Start()
	if err != nil {
		return tools.Result{
			Content: fmt.Sprintf("exec error: %v", err),
			IsError: true,
		}, nil
	}
	err = cmd.Wait()
	output := buf.String()
	// a backgrounded child kept the pipes open after bash exited cleanly:
	// the command itself succeeded, so report success with a note.
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil
		stripNote = joinNote(stripNote, "[a background process still holds the output pipe; returned without waiting for it]")
	}

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		return tools.Result{
			Content: fmt.Sprintf("timeout after %s\n%s", timeout, output),
			IsError: true,
		}, nil
	case ctx.Err() != nil:
		// parent cancellation: propagate up
		return tools.Result{Content: ctx.Err().Error(), IsError: true}, ctx.Err()
	case err != nil:
		// cmd.Wait() may return EPERM when the process group is already dead
		// (cancel → killProcessGroup → cmd.Wait() tries another kill).
		if strings.Contains(err.Error(), "operation not permitted") {
			return tools.Result{
				Content: fmt.Sprintf("timeout after %s\n%s", timeout, output),
				IsError: true,
			}, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return tools.Result{
				Content: fmt.Sprintf("exit %d\n%s", exitErr.ExitCode(), output),
				IsError: true,
			}, nil
		}
		return tools.Result{
			Content: fmt.Sprintf("exec error: %v\n%s", err, output),
			IsError: true,
		}, nil
	}
	if stripNote != "" {
		// prepend so the model sees the nudge even when output is truncated
		// from the tail by the loop's per-tool token cap.
		output = stripNote + "\n" + output
	}
	return tools.Result{Content: output}, nil
}

// buildInvocation picks the shell binary and wraps cmdStr with an optional
// rc-source prelude when Options.UseUserRC is set. Default returns ("bash",
// cmdStr) — unchanged hermetic behavior.
func (t *Tool) buildInvocation(cmdStr string) (string, string) {
	if !t.opts.UseUserRC {
		return "bash", cmdStr
	}
	shellBin := t.opts.Shell
	if shellBin == "" {
		shellBin = os.Getenv("SHELL")
	}
	if shellBin == "" {
		shellBin = "bash"
	}
	rc := t.opts.RCFile
	if rc == "" {
		rc = defaultRCFor(shellBin)
	}
	if rc == "" {
		return shellBin, cmdStr
	}
	// non-interactive shells alias-expand at parse time, so an alias defined
	// while sourcing the rc is invisible to commands tokenized in the same
	// -c string. Workaround: source rc, then run the user command via `eval`
	// — eval parses its argument after the alias table is populated. bash
	// additionally needs `shopt -s expand_aliases` to honor aliases under
	// non-interactive mode at all. The [ -f rc ] guard keeps missing rc
	// files silent for first-run users.
	quotedRC := shellQuote(rc)
	prelude := fmt.Sprintf("[ -f %s ] && . %s 2>/dev/null; ", quotedRC, quotedRC)
	if filepath.Base(shellBin) == "bash" {
		prelude = "shopt -s expand_aliases 2>/dev/null; " + prelude
	}
	return shellBin, prelude + "eval " + shellQuote(cmdStr)
}

// defaultRCFor maps the shell binary to the canonical interactive rc file.
// Returns "" for unsupported shells (caller falls through to no prelude).
func defaultRCFor(shellBin string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	switch filepath.Base(shellBin) {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	}
	return ""
}

// shellQuote wraps s in single quotes for safe inclusion in a shell command,
// escaping any embedded single quotes via the standard '\” dance.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// mergeDangers appends the entries of extra whose key is not already in base.
func mergeDangers(base, extra []safety.Danger) []safety.Danger {
	for _, d := range extra {
		dup := false
		for _, b := range base {
			if b.Key == d.Key {
				dup = true
				break
			}
		}
		if !dup {
			base = append(base, d)
		}
	}
	return base
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}

// untrustedKey scopes a grant to this exact command line. Keying on the
// program name would let one approval of `bash` or `cd` cover every later
// command that starts the same way.
func untrustedKey(cmd string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(cmd)))
	return "untrusted:" + hex.EncodeToString(sum[:])
}
