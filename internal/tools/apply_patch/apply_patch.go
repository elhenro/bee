// Package apply_patch implements the unified-diff mutation tool.
//
// Single primitive: takes a unified diff, applies it to the working tree.
// Creates new files, modifies existing, deletes (when patch wipes content
// or marks file deleted). Fails loud on context mismatch — no fuzzy retry.
package apply_patch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "apply_patch"

// Tool is the apply_patch tool.
type Tool struct {
	pathRe *regexp.Regexp
}

// New returns a fresh apply_patch tool.
func New() tools.Tool { return NewWithFilter(nil) }

// NewWithFilter constructs the apply_patch tool with an optional path regex.
// When pathRe is nil, all paths are allowed (existing behavior).
// When pathRe is non-nil, the whole batch is rejected if ANY file path in
// the patch fails the match — no file is touched on rejection. Paths are
// matched relative to the current working directory when possible.
func NewWithFilter(pathRe *regexp.Regexp) tools.Tool {
	return &Tool{pathRe: pathRe}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Apply a unified diff to the working tree. Creates new files, " +
			"modifies existing ones, or deletes them. Fails loudly when context lines " +
			"don't match — the patch must be precise.",
		PromptSnippet: "Apply unified diff to create/edit/delete files",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"patch": map[string]any{
					"type":        "string",
					"description": "Unified diff (git-style) describing the changes.",
				},
			},
			"required": []string{"patch"},
		},
	}
}

// fileChange records one applied file mutation for the summary.
type fileChange struct {
	path    string
	kind    string // "create", "modify", "delete"
	added   int
	removed int
}

// Run parses the patch and applies each file.
func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	patchStr, ok := input["patch"].(string)
	if !ok || strings.TrimSpace(patchStr) == "" {
		return tools.Result{Content: "missing or empty 'patch' field", IsError: true}, nil
	}

	files, _, err := gitdiff.Parse(strings.NewReader(patchStr))
	if err != nil && isHunkCountErr(err) {
		// LLMs frequently miscount hunk headers (`@@ -a,b +c,d @@`).
		// recompute counts from the body and retry once.
		repaired := repairHunkCounts(patchStr)
		if repaired != patchStr {
			if f2, _, err2 := gitdiff.Parse(strings.NewReader(repaired)); err2 == nil {
				files, err = f2, nil
			}
		}
	}
	if err != nil {
		hint := "parse error: " + err.Error() + "\n" +
			"hunk header or line prefix malformed. for small in-file edits, prefer edit_diff " +
			"(literal find/replace) or hashline_edit (LINE#ID anchors from read with hashline=true). " +
			"if you must use apply_patch, re-read the target with read first and copy exact context lines."
		return tools.Result{Content: hint, IsError: true}, nil
	}
	if len(files) == 0 {
		return tools.Result{Content: "patch contained no file diffs", IsError: true}, nil
	}

	// strip git-style a/ and b/ prefixes; gitdiff only strips when the
	// "diff --git" header is present, but models often emit bare unified
	// diffs with only --- a/path / +++ b/path.
	for _, f := range files {
		f.OldName = stripDiffPrefix(f.OldName)
		f.NewName = stripDiffPrefix(f.NewName)
	}

	if t.pathRe != nil {
		cwd, _ := os.Getwd()
		for _, f := range files {
			p := fileLabel(f)
			match := p
			if cwd != "" && filepath.IsAbs(p) {
				if rel, err := filepath.Rel(cwd, p); err == nil {
					match = rel
				}
			}
			if !t.pathRe.MatchString(match) {
				return tools.Result{Content: fmt.Sprintf("path %q denied by write filter", match), IsError: true}, nil
			}
		}
	}

	changes := make([]fileChange, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, err
		}
		ch, err := applyOne(f)
		if err != nil {
			return tools.Result{
				Content: fmt.Sprintf("apply failed for %s: %v", fileLabel(f), err),
				IsError: true,
			}, nil
		}
		changes = append(changes, ch)
	}

	return tools.Result{Content: summarize(changes)}, nil
}

// applyOne handles a single parsed File entry.
func applyOne(f *gitdiff.File) (fileChange, error) {
	if f.IsBinary {
		return fileChange{}, errors.New("binary patches not supported")
	}

	switch {
	case f.IsDelete:
		return applyDelete(f)
	case f.IsNew:
		return applyCreate(f)
	default:
		return applyModify(f)
	}
}

func applyDelete(f *gitdiff.File) (fileChange, error) {
	path := f.OldName
	if path == "" {
		return fileChange{}, errors.New("delete patch missing old name")
	}
	if err := os.Remove(path); err != nil {
		return fileChange{}, err
	}
	removed := countLines(f, lineRemoved)
	return fileChange{path: path, kind: "delete", removed: removed}, nil
}

func applyCreate(f *gitdiff.File) (fileChange, error) {
	path := f.NewName
	if path == "" {
		return fileChange{}, errors.New("create patch missing new name")
	}
	if _, err := os.Stat(path); err == nil {
		return fileChange{}, fmt.Errorf("create patch but %s already exists", path)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fileChange{}, err
		}
	}
	var buf bytes.Buffer
	if err := gitdiff.Apply(&buf, bytes.NewReader(nil), f); err != nil {
		return fileChange{}, err
	}
	mode := f.NewMode
	if mode == 0 {
		mode = 0o644
	}
	if err := os.WriteFile(path, buf.Bytes(), mode); err != nil {
		return fileChange{}, err
	}
	return fileChange{path: path, kind: "create", added: countLines(f, lineAdded)}, nil
}

func applyModify(f *gitdiff.File) (fileChange, error) {
	path := f.NewName
	if path == "" {
		path = f.OldName
	}
	if path == "" {
		return fileChange{}, errors.New("modify patch missing file name")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return fileChange{}, err
	}
	var buf bytes.Buffer
	if err := gitdiff.Apply(&buf, bytes.NewReader(src), f); err != nil {
		return fileChange{}, err
	}
	// reject empty result on modify; require explicit deletion semantics
	if buf.Len() == 0 && !f.IsDelete {
		return fileChange{}, fmt.Errorf("modify patch for %s produced empty file; use deleted file mode to remove", path)
	}
	// preserve original file mode; fall back to 0o644 if stat fails
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, buf.Bytes(), mode); err != nil {
		return fileChange{}, err
	}
	return fileChange{
		path:    path,
		kind:    "modify",
		added:   countLines(f, lineAdded),
		removed: countLines(f, lineRemoved),
	}, nil
}

type lineKind int

const (
	lineAdded lineKind = iota
	lineRemoved
)

func countLines(f *gitdiff.File, kind lineKind) int {
	n := 0
	for _, frag := range f.TextFragments {
		switch kind {
		case lineAdded:
			n += int(frag.LinesAdded)
		case lineRemoved:
			n += int(frag.LinesDeleted)
		}
	}
	return n
}

func fileLabel(f *gitdiff.File) string {
	if f.NewName != "" {
		return f.NewName
	}
	return f.OldName
}

func summarize(changes []fileChange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "applied %d file(s):\n", len(changes))
	for _, c := range changes {
		switch c.kind {
		case "create":
			fmt.Fprintf(&b, "  + %s (+%d)\n", c.path, c.added)
		case "delete":
			fmt.Fprintf(&b, "  - %s (-%d)\n", c.path, c.removed)
		default:
			fmt.Fprintf(&b, "  ~ %s (+%d -%d)\n", c.path, c.added, c.removed)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
