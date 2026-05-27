// Package ls implements the ls tool: list a single directory (no recursion).
package ls

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "ls"

// maxEntries caps directory listings, matching find's 500 limit.
const maxEntries = 500

// Tool is the ls tool.
type Tool struct {
	root string
}

// New returns an ls tool rooted at root.
func New(root string) *Tool { return &Tool{root: root} }

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "List entries of a directory (no recursion). One line per entry: <d|f|l>\\t<size>\\t<name>. Args: path (optional, default '.').",
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
	abs := path
	if abs == "" {
		abs = t.root
	} else if !filepath.IsAbs(abs) {
		abs = filepath.Join(t.root, abs)
	}
	abs = filepath.Clean(abs)

	rootAbs, err := filepath.Abs(t.root)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return tools.Result{
			Content: fmt.Sprintf("path %q escapes workspace root %q (resolved %q). use a path relative to workspace root, or under %s.",
				path, rootAbs, abs, rootAbs),
			IsError: true,
		}, nil
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	total := len(entries)
	truncated := 0
	if total > maxEntries {
		truncated = total - maxEntries
		entries = entries[:maxEntries]
	}

	var b strings.Builder
	for _, e := range entries {
		kind := "f"
		if e.Type()&os.ModeSymlink != 0 {
			kind = "l"
		} else if e.IsDir() {
			kind = "d"
		}
		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		fmt.Fprintf(&b, "%s\t%d\t%s\n", kind, size, e.Name())
	}
	out := strings.TrimRight(b.String(), "\n")
	if truncated > 0 {
		if out != "" {
			out += "\n"
		}
		out += fmt.Sprintf("(truncated; %d more)", truncated)
	}
	if out == "" {
		return tools.Result{Content: "empty directory"}, nil
	}
	return tools.Result{Content: out}, nil
}
