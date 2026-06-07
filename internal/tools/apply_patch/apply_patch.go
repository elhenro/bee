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
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "apply_patch"

// Tool is the apply_patch tool.
type Tool struct {
	root   string
	pathRe *regexp.Regexp
}

// New returns an apply_patch tool rooted at root (the workspace). Every patch
// target is contained to root.
func New(root string) tools.Tool { return NewWithFilter(root, nil) }

// NewWithFilter constructs the apply_patch tool with an optional path regex.
// When pathRe is nil, all in-root paths are allowed. When non-nil, the whole
// batch is rejected if ANY file path in the patch fails the match. Either way,
// any target that escapes root (absolute or via ..) is refused and no file is
// touched.
func NewWithFilter(root string, pathRe *regexp.Regexp) tools.Tool {
	return &Tool{root: root, pathRe: pathRe}
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
					"minLength":   1,
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

	// under a confined scope, contain every patch target to the workspace root
	// and reject sensitive targets BEFORE touching disk. stripDiffPrefix passes
	// absolute paths through and never strips "..", so without this a header like
	// `+++ b/../../.ssh/authorized_keys` would write outside the tree. Rewrite
	// each name to its absolute form so plan/commit stay consistent. In danger-
	// full-access (empty root) ResolveMaybe is a passthrough and CheckWritable is
	// skipped — patch anywhere.
	for _, f := range files {
		for _, np := range []*string{&f.OldName, &f.NewName} {
			name := *np
			if name == "" || name == "/dev/null" {
				continue
			}
			abs, _, rootAbs, ok := tools.ResolveMaybe(t.root, name)
			if !ok {
				return tools.Result{Content: fmt.Sprintf("patch path %q escapes workspace root %q; refused (no files changed)", name, rootAbs), IsError: true}, nil
			}
			if t.root != "" {
				if err := safety.CheckWritable(abs); err != nil {
					return tools.Result{Content: fmt.Sprintf("patch path %q refused: %v (no files changed)", name, err), IsError: true}, nil
				}
			}
			*np = abs
		}
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

	// plan all changes in memory first; commit only once every file validates,
	// so a mid-batch failure (e.g. context mismatch on the 3rd file) leaves the
	// tree untouched instead of half-patched.
	plans := make([]plannedChange, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, err
		}
		p, err := planOne(f)
		if err != nil {
			return tools.Result{
				Content: fmt.Sprintf("apply failed for %s: %v (no files changed)", fileLabel(f), err),
				IsError: true,
			}, nil
		}
		plans = append(plans, p)
	}
	changes := make([]fileChange, 0, len(plans))
	for i, p := range plans {
		if err := commit(p); err != nil {
			return tools.Result{
				Content: fmt.Sprintf("apply failed writing %s: %v (%d/%d already written)", p.path, err, i, len(plans)),
				IsError: true,
			}, nil
		}
		changes = append(changes, fileChange{path: tools.RelTo(t.root, p.path), kind: p.kind, added: p.added, removed: p.removed})
	}

	return tools.Result{Content: summarize(changes)}, nil
}

// plannedChange is a fully-computed file mutation held in memory until every
// file in the batch validates, so a mid-batch failure can't leave a partially
// applied tree (all-or-nothing).
type plannedChange struct {
	path    string
	kind    string // "create", "modify", "delete"
	data    []byte // bytes to write for create/modify
	mode    os.FileMode
	added   int
	removed int
}

// planOne validates one parsed File and computes its result WITHOUT writing to
// disk. Reads are allowed (modify needs current content); no mutation happens
// here so the caller can abort the whole batch before any commit.
func planOne(f *gitdiff.File) (plannedChange, error) {
	if f.IsBinary {
		return plannedChange{}, errors.New("binary patches not supported")
	}
	switch {
	case f.IsDelete:
		path := f.OldName
		if path == "" {
			return plannedChange{}, errors.New("delete patch missing old name")
		}
		if _, err := os.Stat(path); err != nil {
			return plannedChange{}, err
		}
		return plannedChange{path: path, kind: "delete", removed: countLines(f, lineRemoved)}, nil
	case f.IsNew:
		path := f.NewName
		if path == "" {
			return plannedChange{}, errors.New("create patch missing new name")
		}
		if _, err := os.Stat(path); err == nil {
			return plannedChange{}, fmt.Errorf("create patch but %s already exists", path)
		}
		var buf bytes.Buffer
		if err := gitdiff.Apply(&buf, bytes.NewReader(nil), f); err != nil {
			return plannedChange{}, err
		}
		// ignore the patch-supplied mode: a model-controlled file mode could set
		// over-permissive bits. New files get a fixed safe mode.
		return plannedChange{path: path, kind: "create", data: buf.Bytes(), mode: 0o644, added: countLines(f, lineAdded)}, nil
	default:
		path := f.NewName
		if path == "" {
			path = f.OldName
		}
		if path == "" {
			return plannedChange{}, errors.New("modify patch missing file name")
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return plannedChange{}, err
		}
		var buf bytes.Buffer
		if err := gitdiff.Apply(&buf, bytes.NewReader(src), f); err != nil {
			return plannedChange{}, err
		}
		// reject empty result on modify; require explicit deletion semantics
		if buf.Len() == 0 && !f.IsDelete {
			return plannedChange{}, fmt.Errorf("modify patch for %s produced empty file; use deleted file mode to remove", path)
		}
		// preserve original file mode; fall back to 0o644 if stat fails
		mode := os.FileMode(0o644)
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		return plannedChange{path: path, kind: "modify", data: buf.Bytes(), mode: mode, added: countLines(f, lineAdded), removed: countLines(f, lineRemoved)}, nil
	}
}

// commit writes one planned change to disk.
func commit(p plannedChange) error {
	switch p.kind {
	case "delete":
		return os.Remove(p.path)
	case "create":
		if dir := filepath.Dir(p.path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		return os.WriteFile(p.path, p.data, p.mode)
	default: // modify
		return os.WriteFile(p.path, p.data, p.mode)
	}
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
