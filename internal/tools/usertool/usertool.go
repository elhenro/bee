// Package usertool registers user-defined shell-alias tools loaded from
// [[user_tools]] entries in ~/.bee/config.toml. Each entry maps a tool name
// to a fixed bash command; optional `args` from the model are appended as a
// space-separated suffix so the tool can stay parameterised.
package usertool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const (
	defaultTimeout = 30 * time.Second
	maxOutputBytes = 20 * 1024
	truncMarker    = "\n[…truncated]"
)

// Tool wraps a fixed bash command as a tool the model can invoke.
type Tool struct {
	name        string
	command     string
	description string
}

// New builds a usertool from a name + command template + description.
func New(name, command, description string) (tools.Tool, error) {
	name = strings.TrimSpace(name)
	command = strings.TrimSpace(command)
	if name == "" {
		return nil, errors.New("usertool: empty name")
	}
	if command == "" {
		return nil, errors.New("usertool: empty command")
	}
	desc := description
	if desc == "" {
		desc = "User-defined shell alias for `" + command + "`."
	}
	return &Tool{name: name, command: command, description: desc}, nil
}

func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.name,
		Description: t.description + " Optional `args` string is appended to the command verbatim.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"args": map[string]any{
					"type":        "string",
					"description": "Optional extra args appended to the command, space-prefixed.",
				},
			},
		},
	}
}

func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	cmdStr := t.command
	if v, ok := input["args"].(string); ok {
		if v = strings.TrimSpace(v); v != "" {
			cmdStr = cmdStr + " " + v
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "bash", "-c", cmdStr)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	output := truncateOut(buf.Bytes())
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return tools.Result{Content: fmt.Sprintf("timeout after %s\n%s", defaultTimeout, output), IsError: true}, nil
	case ctx.Err() != nil:
		return tools.Result{Content: ctx.Err().Error(), IsError: true}, ctx.Err()
	case err != nil:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return tools.Result{Content: fmt.Sprintf("exit %d\n%s", exitErr.ExitCode(), output), IsError: true}, nil
		}
		return tools.Result{Content: fmt.Sprintf("exec error: %v\n%s", err, output), IsError: true}, nil
	}
	return tools.Result{Content: output}, nil
}

func truncateOut(b []byte) string {
	if len(b) <= maxOutputBytes {
		return string(b)
	}
	return string(b[:maxOutputBytes]) + truncMarker
}
