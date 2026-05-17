// Package find implements the find tool: recursive name-glob file search.
package find

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "find"

// Tool is the find tool.
type Tool struct {
	root string
	max  int
}

// New returns a find tool rooted at root.
func New(root string) *Tool { return &Tool{root: root, max: 500} }

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        toolName,
		Description:   "Find files by name glob (filepath.Match, e.g. '*.go'). Returns up to 500 paths. Args: name (required), path (optional).",
		PromptSnippet: "Find files by glob pattern (respects .gitignore)",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		},
	}
}

// Run executes the search.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	name, _ := in["name"].(string)
	if name == "" {
		return tools.Result{Content: "missing name", IsError: true}, nil
	}
	// validate the pattern up front
	if _, err := filepath.Match(name, "x"); err != nil {
		return tools.Result{Content: "bad pattern: " + err.Error(), IsError: true}, nil
	}
	root, _ := in["path"].(string)
	if root == "" {
		root = t.root
	}

	var out []string
	count := 0
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		ok, err := filepath.Match(name, filepath.Base(p))
		if err != nil {
			return err
		}
		if ok {
			if count >= t.max {
				return filepath.SkipAll
			}
			out = append(out, tools.RelTo(root, p))
			count++
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return tools.Result{Content: walkErr.Error(), IsError: true}, nil
	}
	if len(out) == 0 {
		return tools.Result{Content: "no matches"}, nil
	}
	return tools.Result{Content: strings.Join(out, "\n")}, nil
}
