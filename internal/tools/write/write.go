// Package write implements the write tool: overwrite a file inside the workspace root.
package write

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "write"

// Tool is the write tool.
type Tool struct {
	root   string
	pathRe *regexp.Regexp
}

// New returns a write tool rooted at root. Writes outside root are refused.
func New(root string) *Tool { return NewWithFilter(root, nil) }

// NewWithFilter constructs the write tool with an optional path regex.
// When pathRe is nil, all paths are allowed (existing behavior).
// When pathRe is non-nil, writes to paths that do NOT match the regex
// (relative to root) are rejected with a clear error and no file is touched.
func NewWithFilter(root string, pathRe *regexp.Regexp) *Tool {
	return &Tool{root: root, pathRe: pathRe}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        toolName,
		Description:   "Overwrite a file with content. Creates parent dirs. Path must stay inside workspace root. Args: path (required), content (required).",
		PromptSnippet: "Create or overwrite files",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}
}

// Run writes content to path after sandbox check.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	path, _ := in["path"].(string)
	if path == "" {
		return tools.Result{Content: "missing path", IsError: true}, nil
	}
	content, ok := in["content"].(string)
	if !ok {
		return tools.Result{Content: "missing content", IsError: true}, nil
	}
	// guard: small models sometimes drop a unified diff into content,
	// destroying the file. detect canonical patch headers and redirect.
	if isDiffContent(content) {
		return tools.Result{
			Content: "refusing to write — content looks like a unified diff. Use the `edit` tool for targeted find/replace, or enable the large profile and use `apply_patch` for unified diffs. `write` overwrites the whole file with raw content.",
			IsError: true,
		}, nil
	}

	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(t.root, path)
	}
	abs = filepath.Clean(abs)

	rootAbs, err := filepath.Abs(t.root)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return tools.Result{Content: "path escapes workspace root", IsError: true}, nil
	}

	if t.pathRe != nil {
		match := rel
		if err != nil {
			match = abs
		}
		if !t.pathRe.MatchString(match) {
			return tools.Result{Content: fmt.Sprintf("path %q denied by write filter", match), IsError: true}, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(content), abs)}, nil
}

// isDiffContent reports whether content looks like a unified diff or
// codex-style patch envelope rather than full file content. Conservative:
// only matches canonical headers so we don't false-positive on docs that
// happen to start with `---` (YAML frontmatter starts the same way but
// continues with a key:value line, not `+++`).
func isDiffContent(content string) bool {
	trimmed := strings.TrimLeft(content, "\n\r\t ")
	if strings.HasPrefix(trimmed, "*** Begin Patch") {
		return true
	}
	// unified diff: `--- a/...` then `+++ b/...` within the first ~5 lines.
	lines := strings.SplitN(trimmed, "\n", 6)
	hasMinus := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "--- ") {
			hasMinus = true
			continue
		}
		if hasMinus && strings.HasPrefix(ln, "+++ ") {
			return true
		}
	}
	// hunk header with no surrounding file headers — still smells like a diff.
	for _, ln := range lines {
		if strings.HasPrefix(ln, "@@ ") && strings.Contains(ln, " @@") {
			return true
		}
	}
	return false
}
