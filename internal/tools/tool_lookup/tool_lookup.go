// Package tool_lookup implements the tool_lookup tool: returns the full
// schema, description, and prompt snippet of any registered tool by name.
//
// Use case: after auto-compact the tool manifest may be summarized or out
// of cache. tool_lookup lets the model re-acquire one tool's full spec on
// demand instead of paying the prefix cost of advertising every tool's
// full schema every turn. Useful for tiny-profile models that drop most
// per-parameter descriptions to save tokens.
package tool_lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "tool_lookup"

// SpecProvider is the subset of *tools.Registry the lookup tool reads. The
// indirection keeps tests simple and avoids a circular dependency at the
// registry-construction site.
type SpecProvider interface {
	Specs() []llm.ToolSpec
	Names() []string
}

// Tool is the tool_lookup tool.
type Tool struct {
	reg SpecProvider
}

// New returns a tool_lookup bound to reg. reg must outlive the tool.
func New(reg SpecProvider) *Tool { return &Tool{reg: reg} }

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "Return the full description and JSON Schema for any registered tool. Omit `name` to list every tool name. Args: name (optional).",
		PromptSnippet: "look up a tool's full schema by name",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "name of the tool to look up. Omit to list every registered tool name.",
				},
			},
		},
	}
}

// Run handles two modes: name="" lists all registered tool names; name=X
// returns the full Spec of tool X (Description + Schema) as JSON.
func (t *Tool) Run(_ context.Context, in map[string]any) (tools.Result, error) {
	if t.reg == nil {
		return tools.Result{Content: "tool_lookup: no registry bound", IsError: true}, nil
	}
	name, _ := in["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		names := t.reg.Names()
		sort.Strings(names)
		return tools.Result{Content: strings.Join(names, "\n")}, nil
	}
	for _, s := range t.reg.Specs() {
		if s.Name != name {
			continue
		}
		out := map[string]any{
			"name":        s.Name,
			"description": s.Description,
			"schema":      s.Schema,
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
		return tools.Result{Content: string(b)}, nil
	}
	return tools.Result{Content: fmt.Sprintf("unknown tool %q", name), IsError: true}, nil
}
