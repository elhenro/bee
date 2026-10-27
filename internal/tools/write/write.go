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

// maxRewriteBytes caps the content size for a write to an EXISTING file. above
// this, the model is most likely trying to "rewrite the whole file" — a
// pattern that wedges: the model also tends to emit the file content as text
// preamble, blowing the output budget before the tool call JSON completes,
// which surfaces as ErrProviderReject. force it onto `edit` (small targeted
// change) or `shell`+`git checkout` (full rollback) for existing files. new
// files are uncapped since there is no prior version to compare against.
const maxRewriteBytes = 8 * 1024

// Tool is the write tool.
type Tool struct {
	root   string
	pathRe *regexp.Regexp
}

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
		Name:          toolName,
		Description:   "Overwrite a file with content. Creates parent dirs. Path must stay inside workspace root. Empty content is rejected (file deletion is not supported). Args: path (required), content (required, non-empty).",
		PromptSnippet: "Create or overwrite files",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "minLength": 1},
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
	if content == "" {
		return tools.Result{Content: "content must be non-empty", IsError: true}, nil
	}
	// guard: small models sometimes drop a unified diff into content,
	// destroying the file. detect canonical patch headers and redirect.
	if isDiffContent(content) {
		return tools.Result{Content: "refusing to write — content looks like a unified diff, not file contents. Use the `edit` tool to apply a targeted change, or pass the full final file as raw text to `write` (no diff markers). `write` overwrites the whole file verbatim.", IsError: true}, nil
	}

	// ResolveMaybe contains the write to the workspace root only under a confined
	// scope (non-empty root); in danger-full-access (empty root) it resolves the
	// path absolute and accepts it, preserving bee's default "write anywhere".
	abs, rel, rootAbs, ok := tools.ResolveMaybe(t.root, path)
	if !ok {
		return tools.Result{
			Content: fmt.Sprintf("path %q escapes workspace root %q (resolved %q). use a path relative to workspace root, or under %s.",
				path, rootAbs, abs, rootAbs),
			IsError: true,
		}, nil
	}

	abs, msg := tools.CheckWriteTarget(t.root, abs, t.pathRe)
	if msg != "" {
		return tools.Result{Content: msg, IsError: true}, nil
	}

	// single stat: drives both the large-rewrite guard and the mode preservation.
	// new files default to 0o644; existing files keep their mode (so 0o755 bits
	// survive a content overwrite).
	var existed bool
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(abs); statErr == nil {
		existed = true
		mode = info.Mode().Perm()
	}
	// large-rewrite guard: refuse whole-file rewrites of existing files above
	// the cap. the wedge pattern is "model prints the file as text preamble,
	// burns output budget, then emits a write call with truncated args" → 400.
	// forcing it to edit / git checkout breaks the loop at the tool layer.
	if existed && len(content) > maxRewriteBytes {
		return tools.Result{
			Content: fmt.Sprintf("refusing to write %d bytes to existing file %q — `write` is for new files or small rewrites only. "+
				"for targeted changes use the `edit` tool; for full rollbacks use `shell` with `git checkout -- %s`. (cap=%d bytes)",
				len(content), path, rel, maxRewriteBytes),
			IsError: true,
		}, nil
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	if err := os.WriteFile(abs, []byte(content), mode); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(content), abs)}, nil
}

// isDiffContent reports whether content looks like a unified diff or
// Begin/End Patch envelope rather than full file content. Conservative:
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
