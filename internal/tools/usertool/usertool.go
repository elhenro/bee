// Package usertool registers user-defined shell-alias tools loaded from
// [[user_tools]] entries in ~/.bee/config.toml. Each entry maps a tool name
// to a fixed bash command; optional `args` from the model are parsed
// POSIX-style and passed as positional parameters ($1, $@) so model input
// cannot inject shell metacharacters.
package usertool

import (
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
	defaultTimeout = 120 * time.Second
	maxOutputBytes = 20 * 1024
)

// Tool wraps a fixed bash command as a tool the model can invoke.
type Tool struct {
	name        string
	command     string
	description string
	wrap        func(argv []string) []string
}

// Option tunes a Tool at construction.
type Option func(*Tool)

// WithWrap runs the command through w (the sandbox wrapper) instead of bare
// bash. A nil w leaves the tool unwrapped.
func WithWrap(w func(argv []string) []string) Option {
	return func(t *Tool) { t.wrap = w }
}

// New builds a usertool from a name + command template + description.
func New(name, command, description string, opts ...Option) (tools.Tool, error) {
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
	t := &Tool{name: name, command: command, description: desc}
	for _, o := range opts {
		o(t)
	}
	return t, nil
}

func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.name,
		Description: t.description + " Optional `args` string is parsed POSIX-style and passed as positional parameters ($1, $@); shell metacharacters in args are not interpreted.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"args": map[string]any{
					"type":        "string",
					"description": "Optional extra args, parsed shell-style and passed as positional parameters.",
				},
			},
		},
	}
}

func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	var extra []string
	if raw, present := input["args"]; present {
		switch v := raw.(type) {
		case string:
			parsed, err := splitArgs(v)
			if err != nil {
				return tools.Result{Content: fmt.Sprintf("args parse error: %v", err), IsError: true}, nil
			}
			extra = parsed
		case nil:
			// treat as absent
		default:
			return tools.Result{Content: fmt.Sprintf("args must be a string, got %T", raw), IsError: true}, nil
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	// pass extras as positional params so $1..$N and "$@" expand in the
	// user-registered command but model input is never re-parsed by bash
	argv := append([]string{"bash", "-c", t.command, "bee-usertool"}, extra...)
	if t.wrap != nil {
		argv = t.wrap(argv)
	}
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	buf := tools.NewCapBuffer(maxOutputBytes, 0)
	cmd.Stdout = buf
	cmd.Stderr = buf
	// own process group + pipe wait cap, like the shell tool: cancel kills
	// backgrounded children and a lingering one cannot hold Wait open.
	tools.GroupKill(cmd, 2*time.Second)
	err := cmd.Run()
	output := buf.String()
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil // exited cleanly; only a leftover child held the pipe
	}
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
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

// SplitArgs is splitArgs for callers that vet the fields before running.
func SplitArgs(s string) ([]string, error) { return splitArgs(s) }

// splitArgs parses a POSIX-ish argument string into fields, honoring
// single quotes, double quotes, and backslash escapes. It does NOT
// expand variables or globs; values are treated as literal data.
func splitArgs(s string) ([]string, error) {
	var (
		out      []string
		cur      strings.Builder
		inSingle bool
		inDouble bool
		hasField bool
	)
	flush := func() {
		if hasField {
			out = append(out, cur.String())
			cur.Reset()
			hasField = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
				continue
			}
			cur.WriteByte(c)
			hasField = true
		case inDouble:
			if c == '\\' && i+1 < len(s) {
				next := s[i+1]
				if next == '"' || next == '\\' || next == '$' || next == '`' || next == '\n' {
					cur.WriteByte(next)
					i++
					hasField = true
					continue
				}
				cur.WriteByte(c)
				hasField = true
				continue
			}
			if c == '"' {
				inDouble = false
				continue
			}
			cur.WriteByte(c)
			hasField = true
		default:
			switch c {
			case ' ', '\t', '\n':
				flush()
			case '\'':
				inSingle = true
				hasField = true
			case '"':
				inDouble = true
				hasField = true
			case '\\':
				if i+1 < len(s) {
					cur.WriteByte(s[i+1])
					i++
					hasField = true
				}
			default:
				cur.WriteByte(c)
				hasField = true
			}
		}
	}
	if inSingle || inDouble {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return out, nil
}
