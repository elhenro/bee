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

const toolName = "glob"

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
		Name:          toolName,
		Description:   "Filename glob (filepath.Match on basename, e.g. '*.go', '*_test.go'). Auto-excludes .claude, vendor, node_modules, testdata and dedupes worktrees. Leading '**/' is accepted and stripped (recursion is implicit). Mid-path '**' is supported: 'src/**/*.go' splits into a directory prefix filter ('src/') and a basename glob ('*.go'). Returns up to 500 paths. Args: pattern (required, alias: name), path (optional).",
		PromptSnippet: "find files by name pattern",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "glob pattern, e.g. '*.go' or '**/foo*.ts'"},
				"path":    map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	}
}

// Run executes the search.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	// accept `pattern` (canonical) or `name` (legacy alias). most upstream
	// tool schemas use `pattern`, and several models (deepseek-v4-flash,
	// gpt-oss) emit `pattern` regardless of what we advertise.
	pat, _ := in["pattern"].(string)
	if pat == "" {
		pat, _ = in["name"].(string)
	}
	if pat == "" {
		return tools.Result{Content: "missing pattern", IsError: true}, nil
	}
	// strip leading '**/' (and bare '**'), recursion is implicit. without
	// this, a pattern like '**/Bunker*.ts' would never match because
	// filepath.Match is applied to the basename only.
	for strings.HasPrefix(pat, "**/") {
		pat = pat[3:]
	}
	if pat == "**" {
		pat = "*"
	}
	// handle mid-path '**' by splitting into a directory prefix filter and
	// a basename glob. filepath.Match has no doublestar support, so
	// 'src/**/*.go' is decomposed into prefix='src/' and basename='*.go'.
	// if multiple '**' segments appear, the first split wins.
	var dirPrefix string
	if i := strings.Index(pat, "**"); i >= 0 {
		dirPrefix = strings.TrimSuffix(pat[:i], "/")
		rest := strings.TrimPrefix(pat[i+2:], "/")
		if rest == "" {
			rest = "*"
		}
		pat = rest
	}
	// validate the pattern up front
	if _, err := filepath.Match(pat, "x"); err != nil {
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
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".claude" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		ok, err := filepath.Match(pat, filepath.Base(p))
		if err != nil {
			return err
		}
		if ok && dirPrefix != "" {
			rel := tools.RelTo(root, p)
			if rel != dirPrefix && !strings.HasPrefix(rel, dirPrefix+string(filepath.Separator)) {
				ok = false
			}
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
