// Package skillexec adapts an exec-kind skill into a model-callable tool.
//
// It reuses usertool for hardened execution (POSIX-parsed positional args,
// injection-safe, timeout, output cap) and adds a run-time hardline safety
// re-check: a route recorded as read-only must not resolve to a catastrophic
// shape later. Output redaction and per-tool truncation are applied by the
// loop, so an exec-skill registered as a tool inherits them for free.
package skillexec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/usertool"
)

// Tool wraps an exec skill as a tool.
type Tool struct {
	command string
	inner   tools.Tool
}

// New builds a tool from an exec-kind skill. Non-exec kinds and empty commands
// are rejected so a misfiled skill fails loudly at registration, not at call.
func New(s skills.Skill) (tools.Tool, error) {
	if s.Kind != skills.KindExec {
		return nil, fmt.Errorf("skillexec: skill %q is kind %q, not exec", s.Name, s.Kind)
	}
	cmd := commandFromExec(s.Exec)
	if strings.TrimSpace(cmd) == "" {
		return nil, errors.New("skillexec: empty exec command")
	}
	inner, err := usertool.New(s.Name, cmd, s.Description)
	if err != nil {
		return nil, err
	}
	return &Tool{command: cmd, inner: inner}, nil
}

func (t *Tool) Spec() llm.ToolSpec { return t.inner.Spec() }

func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	if err := safety.CheckShellCommand(t.command); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	return t.inner.Run(ctx, input)
}

// commandFromExec extracts the bash command string from an exec vector. The
// canonical waggle form is [bash, -c, <script>]; any other vector joins with
// spaces (author-controlled; model args arrive separately as positional params).
func commandFromExec(exec []string) string {
	if len(exec) >= 3 && (exec[0] == "bash" || exec[0] == "sh") && exec[1] == "-c" {
		return strings.Join(exec[2:], " ")
	}
	return strings.Join(exec, " ")
}
