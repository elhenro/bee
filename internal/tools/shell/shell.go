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
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/jobs"
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
//
// Jobs, when non-nil, enables `background: true`: the command is handed to the
// manager and the call returns at once. nil leaves the flag out of the schema.
type Options struct {
	UseUserRC bool
	Shell     string
	RCFile    string
	Allowed   func(cmd string) bool
	Jobs      *jobs.Manager
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
	props := map[string]any{
		"command": map[string]any{
			"type":        "string",
			"minLength":   1,
			"description": "Command line passed to bash -c. Do not prepend `cd <dir> &&` — process cwd is already set.",
		},
		"timeout_seconds": map[string]any{
			"type":        "integer",
			"description": "Hard timeout in seconds (default 120, background 1800). Raise it for long builds or test suites.",
		},
		"cwd": map[string]any{
			"type":        "string",
			"description": "Override working directory. Omit to inherit bee's cwd (the usual case).",
		},
	}
	desc := "Run a shell command via `bash -c`. Combined stdout+stderr returned, capped at 20 KB. Already runs in bee's cwd — do NOT prepend `cd <dir> &&`; use the cwd field only to override."
	if t.opts.Jobs != nil {
		props["background"] = map[string]any{
			"type":        "boolean",
			"description": backgroundHint,
		}
		desc += " Slow command (build, test suite, server)? Set background=true: it returns at once and the result arrives as a new message when it exits. Never sleep or poll."
	}
	return llm.ToolSpec{
		Name:          toolName,
		Description:   desc,
		PromptSnippet: "Execute bash.",
		Schema: map[string]any{
			"type":       "object",
			"properties": props,
			"required":   []string{"command"},
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
	cwd, _ := input["cwd"].(string)
	if cwd == "" {
		cmdStr, stripNote = stripRedundantCd(cmdStr)
	}
	// display command = unwrapped form when engine pre-wrapped with sandbox-exec;
	// otherwise the modal would show the helper profile, not the user's intent.
	displayCmd := cmdStr
	if v, ok := input["_orig_command"].(string); ok && v != "" {
		displayCmd = v
	}
	if res, refused := t.vet(ctx, cmdStr, displayCmd); refused {
		return res, nil
	}

	if wantsBackground(input) {
		return t.runBackground(input, cmdStr, displayCmd, stripNote), nil
	}

	timeout, err := timeoutArg(input, defaultTimeout)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
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

	if err := cmd.Start(); err != nil {
		return tools.Result{Content: fmt.Sprintf("exec error: %v", err), IsError: true}, nil
	}
	err = cmd.Wait()
	// a backgrounded child kept the pipes open after bash exited cleanly:
	// the command itself succeeded, so report success with a note.
	if tools.PipeHeldOpen(cmd, err) {
		err = nil
		if stripNote != "" {
			stripNote += "\n"
		}
		stripNote += "[a background process still holds the output pipe; returned without waiting for it]"
	}
	res, err := tools.CommandResult(ctx, runCtx, err, buf.String(), timeout)
	if !res.IsError && stripNote != "" {
		// prepend so the model sees the nudge even when output is truncated
		// from the tail by the loop's per-tool token cap.
		res.Content = stripNote + "\n" + res.Content
	}
	return res, err
}

// vet runs the safety and approval gates. refused is true with the result to
// hand back when the command must not run.
func (t *Tool) vet(ctx context.Context, cmdStr, displayCmd string) (res tools.Result, refused bool) {
	fail := func(msg string) (tools.Result, bool) { return tools.Result{Content: msg, IsError: true}, true }
	if err := safety.CheckShellCommand(displayCmd); err != nil {
		return fail(err.Error())
	}
	// defense in depth: also vet the command that will actually execute, so a
	// display/exec mismatch (a spoofed _orig_command slipping past input
	// sanitization, or a wrapper bug) can never downgrade the hardline check.
	if cmdStr != displayCmd {
		if err := safety.CheckShellCommand(cmdStr); err != nil {
			return fail(err.Error())
		}
	}
	if t.approver == nil {
		return res, false
	}
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
			return fail(fmt.Sprintf("approval error: %v", err))
		}
		if d == approval.Deny {
			return fail(fmt.Sprintf("refused by user: %s (%s). Try a different approach.", h.Desc, h.Key))
		}
	}
	return res, false
}

// timeoutArg reads timeout_seconds; absent or non-positive keeps def.
func timeoutArg(input map[string]any, def time.Duration) (time.Duration, error) {
	v, ok := input["timeout_seconds"]
	if !ok {
		return def, nil
	}
	var secs int
	switch n := v.(type) {
	case int:
		secs = n
	case int64:
		secs = int(n)
	case float64:
		secs = int(n)
	default:
		return 0, fmt.Errorf("bad timeout_seconds: not a number: %T", v)
	}
	if secs > 0 {
		return time.Duration(secs) * time.Second, nil
	}
	return def, nil
}

// mergeDangers appends the entries of extra whose key is not already in base.
func mergeDangers(base, extra []safety.Danger) []safety.Danger {
	for _, d := range extra {
		if !slices.ContainsFunc(base, func(b safety.Danger) bool { return b.Key == d.Key }) {
			base = append(base, d)
		}
	}
	return base
}

// untrustedKey scopes a grant to this exact command line. Keying on the
// program name would let one approval of `bash` or `cd` cover every later
// command that starts the same way.
func untrustedKey(cmd string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(cmd)))
	return "untrusted:" + hex.EncodeToString(sum[:])
}
