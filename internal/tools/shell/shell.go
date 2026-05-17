// Package shell implements the shell tool: bash -c execution with timeout
// and output truncation. No sandboxing here — slice 1F adds that layer.
//
// non-login non-interactive bash skips ~/.bash_profile and ~/.bashrc, matching
// pi's behavior. avoids tripping over user rc files that misbehave under sandbox.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
)

const (
	toolName       = "bash"
	defaultTimeout = 30 * time.Second
	maxOutputBytes = 20 * 1024
	truncMarker    = "\n[…truncated]"
)

// Tool is the shell executor.
//
// approver, when non-nil, is consulted whenever safety.DetectDangerous flags
// the command. nil approver = no gating (legacy behavior). Hardline checks in
// safety.CheckShellCommand always run regardless of approver.
type Tool struct {
	approver approval.Approver
}

// New returns a shell tool with no approval gating. Use NewWithApprover to
// enable the dangerous-pattern prompt flow.
func New() tools.Tool { return &Tool{} }

// NewWithApprover returns a shell tool that consults app before running any
// command flagged by safety.DetectDangerous. A Deny verdict aborts execution
// and returns an explanatory IsError result.
func NewWithApprover(app approval.Approver) tools.Tool { return &Tool{approver: app} }

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        toolName,
		Description:   "Run a shell command via `bash -c`. Combined stdout+stderr returned, capped at 20 KB.",
		PromptSnippet: "Execute bash commands (ls, grep, find, etc.)",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Command line passed to bash -c.",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Hard timeout in seconds (default 30).",
				},
				"cwd": map[string]any{
					"type":        "string",
					"description": "Optional working directory.",
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
	// display command = unwrapped form when engine pre-wrapped with sandbox-exec;
	// otherwise the modal would show the helper profile, not the user's intent.
	displayCmd := cmdStr
	if v, ok := input["_orig_command"].(string); ok && v != "" {
		displayCmd = v
	}
	if err := safety.CheckShellCommand(displayCmd); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	if t.approver != nil {
		if key, desc, hit := safety.DetectDangerous(displayCmd); hit {
			d, err := t.approver.Request(ctx, displayCmd, key, desc)
			if err != nil {
				return tools.Result{Content: fmt.Sprintf("approval error: %v", err), IsError: true}, nil
			}
			if d == approval.Deny {
				return tools.Result{
					Content: fmt.Sprintf("refused by user: %s (%s). Try a different approach.", desc, key),
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

	cmd := exec.CommandContext(runCtx, "bash", "-c", cmdStr)
	cmd.Dir = cwd
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := truncate(buf.Bytes())

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return tools.Result{
			Content: fmt.Sprintf("timeout after %s\n%s", timeout, output),
			IsError: true,
		}, nil
	case ctx.Err() != nil:
		// parent cancellation: propagate up
		return tools.Result{Content: ctx.Err().Error(), IsError: true}, ctx.Err()
	case err != nil:
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
	return tools.Result{Content: output}, nil
}

func truncate(b []byte) string {
	if len(b) <= maxOutputBytes {
		return string(b)
	}
	return string(b[:maxOutputBytes]) + truncMarker
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
