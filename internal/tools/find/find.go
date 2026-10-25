// Package find implements the find tool: recursive name-glob file search.
package find

import (
	"context"
	"path"
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
				"pattern": map[string]any{"type": "string", "minLength": 1, "description": "glob pattern, e.g. '*.go' or '**/foo*.ts'"},
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
	hasStarStar := strings.Contains(pat, "**")
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
	reqPath, _ := in["path"].(string)
	root, err := tools.ResolveSearchRoot(t.root, reqPath)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}

	var out []string
	err = tools.WalkFiles(ctx, root, func(p string) error {
		rel := tools.RelTo(root, p)
		ok, err := matchPattern(pat, rel, hasStarStar)
		if err != nil {
			return err
		}
		if ok && dirPrefix != "" {
			ok = rel == dirPrefix || strings.HasPrefix(rel, dirPrefix+string(filepath.Separator))
		}
		if ok {
			if len(out) >= t.max {
				return filepath.SkipAll
			}
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	return tools.LinesResult(out), nil
}

// matchPattern matches a glob against a root-relative path. A pattern without
// a slash is a basename glob. With a slash and no '**' it is anchored at the
// root (`pkg/*.go`); after a '**' split the remaining segments match the tail
// of the path.
func matchPattern(pat, rel string, afterStarStar bool) (bool, error) {
	if !strings.Contains(pat, "/") {
		return filepath.Match(pat, filepath.Base(rel))
	}
	rel = filepath.ToSlash(rel)
	if !afterStarStar {
		return path.Match(strings.TrimPrefix(pat, "./"), rel)
	}
	segs := strings.Split(rel, "/")
	n := strings.Count(pat, "/") + 1
	if n > len(segs) {
		return false, nil
	}
	return path.Match(pat, strings.Join(segs[len(segs)-n:], "/"))
}
