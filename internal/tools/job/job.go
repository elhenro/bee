// Package job implements the job tool: list, read and stop the background
// commands started with bash background=true. Completion needs no tool: the
// result arrives as a message on its own.
package job

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/jobs"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "job"

// Tool is the job tool.
type Tool struct{ mgr *jobs.Manager }

// New returns a job tool over mgr.
func New(mgr *jobs.Manager) *Tool { return &Tool{mgr: mgr} }

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "Inspect background commands started with bash background=true. action=list shows all jobs; action=output id=N shows what job N printed so far; action=kill id=N stops it. A job's final result arrives as a message by itself: do not poll with this tool.",
		PromptSnippet: "List, read or stop background jobs.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{"list", "output", "kill"},
				},
				"id": map[string]any{
					"type":        "integer",
					"description": "Job number, for output and kill.",
				},
			},
			"required": []string{"action"},
		},
	}
}

// Run executes one action.
func (t *Tool) Run(_ context.Context, in map[string]any) (tools.Result, error) {
	action, _ := in["action"].(string)
	switch action {
	case "list":
		return tools.Result{Content: t.list()}, nil
	case "output", "kill":
		id, ok := jobID(in["id"])
		if !ok {
			return tools.Result{Content: "missing 'id': the job number from `job list`", IsError: true}, nil
		}
		if action == "kill" {
			return t.kill(id), nil
		}
		return t.output(id), nil
	}
	return tools.Result{Content: "action must be list, output or kill", IsError: true}, nil
}

func (t *Tool) list() string {
	all := t.mgr.List()
	if len(all) == 0 {
		return "no background jobs"
	}
	lines := make([]string, len(all))
	for i, j := range all {
		lines[i] = fmt.Sprintf("#%d %s %s  %s", j.ID, status(j), round(j.Elapsed()), oneLine(j.Label))
	}
	return strings.Join(lines, "\n")
}

func (t *Tool) output(id int) tools.Result {
	j, ok := t.mgr.Get(id)
	if !ok {
		return tools.Result{Content: fmt.Sprintf("no job %d", id), IsError: true}
	}
	head := fmt.Sprintf("job %d %s after %s: %s", j.ID, status(j), round(j.Elapsed()), oneLine(j.Label))
	if j.Output == "" {
		return tools.Result{Content: head + "\n(no output yet)"}
	}
	return tools.Result{Content: head + "\n" + j.Output}
}

func (t *Tool) kill(id int) tools.Result {
	if err := t.mgr.Kill(id); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}
	}
	return tools.Result{Content: fmt.Sprintf("killed job %d", id)}
}

// status is the state word, with the exit code once a command ended on its own.
func status(j jobs.Info) string {
	if j.State == jobs.Exited {
		return fmt.Sprintf("exited %d", j.Exit)
	}
	return string(j.State)
}

func round(d time.Duration) time.Duration {
	if d < time.Minute {
		return d.Round(100 * time.Millisecond)
	}
	return d.Round(time.Second)
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}

// jobID accepts the shapes a model sends for an integer: json number, int, or
// a numeric string.
func jobID(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		var id int
		if _, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(n), "#"), "%d", &id); err == nil {
			return id, true
		}
	}
	return 0, false
}
