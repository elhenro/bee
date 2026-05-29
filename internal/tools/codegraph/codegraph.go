// Package codegraph wraps the external `codegraph` CLI as a bee tool.
//
// Auto-registers when both the `codegraph` binary is on PATH and the project
// has a `.codegraph/codegraph.db` indexed store. One tool, op-dispatched, so
// the prompt manifest stays small while the model gets full query surface
// (search, callers, callees, context, impact, trace, affected, files,
// status, query).
package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "codegraph"

// ops whitelisted for dispatch. keeps shellout safe — model can't pass
// arbitrary subcommands like `install` / `uninstall` that mutate the
// host or rewrite the index.
var allowedOps = map[string]bool{
	"search":   true,
	"query":    true,
	"callers":  true,
	"callees":  true,
	"context":  true,
	"impact":   true,
	"trace":    true,
	"affected": true,
	"files":    true,
	"status":   true,
}

// Available reports whether codegraph integration should be enabled for
// the given working directory. Both conditions must hold: a project-local
// `.codegraph/codegraph.db` store and a `codegraph` binary on PATH.
func Available(cwd string) (binPath string, ok bool) {
	db := filepath.Join(cwd, ".codegraph", "codegraph.db")
	if _, err := os.Stat(db); err != nil {
		return "", false
	}
	bin, err := exec.LookPath("codegraph")
	if err != nil {
		return "", false
	}
	return bin, true
}

// Tool is the codegraph wrapper tool.
type Tool struct {
	cwd     string
	bin     string
	timeout time.Duration
}

// New returns a codegraph tool for cwd. Caller is expected to gate
// construction on Available(cwd) returning ok.
func New(cwd, bin string) *Tool {
	return &Tool{cwd: cwd, bin: bin, timeout: 30 * time.Second}
}

func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Query the project's CodeGraph index for symbol relationships, " +
			"call graphs, and code structure. Faster than grep+read for " +
			"navigating unfamiliar code. " +
			"Args: op (required, one of: search, query, callers, callees, context, impact, trace, affected, files, status), " +
			"target (symbol name, file path, or search text; required for all ops except status), " +
			"args (optional list of extra CLI flags, e.g. ['--limit','20']).",
		PromptSnippet: "query the .codegraph index for symbols/callers/context",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "subcommand: search|query|callers|callees|context|impact|trace|affected|files|status",
				},
				"target": map[string]any{
					"type":        "string",
					"description": "symbol name, file path, or search text",
				},
				"args": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "extra CLI flags passed through verbatim",
				},
			},
			"required": []string{"op"},
		},
	}
}

func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	op, _ := in["op"].(string)
	op = strings.TrimSpace(op)
	if op == "" {
		return tools.Result{Content: "missing op", IsError: true}, nil
	}
	if !allowedOps[op] {
		return tools.Result{Content: fmt.Sprintf("op %q not allowed (use one of: search, query, callers, callees, context, impact, trace, affected, files, status)", op), IsError: true}, nil
	}
	target, _ := in["target"].(string)
	target = strings.TrimSpace(target)
	if target == "" && op != "status" {
		return tools.Result{Content: "missing target", IsError: true}, nil
	}

	argv := []string{op}
	if target != "" {
		argv = append(argv, target)
	}
	if extras, ok := in["args"].([]any); ok {
		for _, a := range extras {
			s, ok := a.(string)
			if !ok || s == "" {
				continue
			}
			argv = append(argv, s)
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, t.bin, argv...)
	cmd.Dir = t.cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		errOut := strings.TrimSpace(stderr.String())
		if errOut == "" {
			errOut = err.Error()
		}
		msg := errOut
		if out != "" {
			msg = out + "\n" + errOut
		}
		return tools.Result{Content: msg, IsError: true}, nil
	}
	if out == "" {
		out = "no results"
	}
	return tools.Result{Content: out}, nil
}
