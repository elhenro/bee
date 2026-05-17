// Package ls implements the ls tool: list a single directory (no recursion).
package ls

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "ls"

// Tool is the ls tool.
type Tool struct {
	root string
}

// New returns an ls tool rooted at root.
func New(root string) *Tool { return &Tool{root: root} }

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        toolName,
		Description:   "List entries of a directory (no recursion). One line per entry: <d|f>\\t<size>\\t<name>. Args: path (optional, default '.').",
		PromptSnippet: "List directory contents",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
		},
	}
}

// Run lists the directory.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	path, _ := in["path"].(string)
	if path == "" {
		path = t.root
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var b strings.Builder
	for _, e := range entries {
		kind := "f"
		if e.IsDir() {
			kind = "d"
		}
		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		fmt.Fprintf(&b, "%s\t%d\t%s\n", kind, size, e.Name())
	}
	return tools.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}
