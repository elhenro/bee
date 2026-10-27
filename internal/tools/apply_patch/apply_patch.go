// Package apply_patch implements the unified-diff mutation tool.
//
// Single primitive: takes a unified diff, applies it to the working tree.
// Creates new files, modifies existing, deletes (when patch wipes content
// or marks file deleted). Fails loud on context mismatch — no fuzzy retry.
package apply_patch

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "apply_patch"

// Tool is the apply_patch tool.
type Tool struct {
	root   string
	pathRe *regexp.Regexp
}

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

	files, err := parsePatch(patchStr)
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
	// diffs with only --- a/path / +++ b/path. stripping again after gitdiff
	// would eat a real top-level dir named a or b.
	if !strings.Contains("\n"+patchStr, "\ndiff --git ") {
		for _, f := range files {
			f.OldName = stripDiffPrefix(f.OldName)
			f.NewName = stripDiffPrefix(f.NewName)
		}
	}

	shown, msg := t.contain(files)
	if msg != "" {
		return tools.Result{Content: msg, IsError: true}, nil
	}

	// plan all changes in memory first; commit only once every file validates,
	// so a mid-batch failure (e.g. context mismatch on the 3rd file) leaves the
	// tree untouched instead of half-patched.
	// sections share one overlay, so two sections for the same file apply in
	// order instead of both planning against the on-disk original.
	ov := newOverlay()
	changes := make([]fileChange, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, err
		}
		c, err := ov.plan(f)
		if err != nil {
			return tools.Result{Content: fmt.Sprintf("apply failed for %s: %v (no files changed)", fileLabel(f), err), IsError: true}, nil
		}
		if orig, ok := shown[c.path]; ok {
			c.path = orig
		}
		c.path = tools.RelTo(t.root, c.path)
		changes = append(changes, c)
	}
	if path, n, total, err := ov.commit(); err != nil {
		return tools.Result{Content: fmt.Sprintf("apply failed writing %s: %v (%d/%d already written)", path, err, n, total), IsError: true}, nil
	}

	return tools.Result{Content: summarize(changes)}, nil
}

// parsePatch parses a unified diff. LLMs frequently miscount hunk headers
// (`@@ -a,b +c,d @@`), so a count error is retried once with the counts
// recomputed from the body.
func parsePatch(patch string) ([]*gitdiff.File, error) {
	files, _, err := gitdiff.Parse(strings.NewReader(patch))
	if err != nil && isHunkCountErr(err) {
		if repaired := repairHunkCounts(patch); repaired != patch {
			if f2, _, err2 := gitdiff.Parse(strings.NewReader(repaired)); err2 == nil {
				return f2, nil
			}
		}
	}
	return files, err
}

// contain, under a confined scope, contains every patch target to the
// workspace root and rejects sensitive targets BEFORE touching disk.
// stripDiffPrefix passes absolute paths through and never strips "..", so
// without this a header like `+++ b/../../.ssh/authorized_keys` would write
// outside the tree. Each name is rewritten to its absolute form so plan/commit
// stay consistent. In danger-full-access (empty root) ResolveMaybe is a
// passthrough and CheckWritable is skipped, so patches land anywhere. shown
// maps each resolved target to the path as given, for the summary; msg is
// non-empty when a target is refused.
func (t *Tool) contain(files []*gitdiff.File) (shown map[string]string, msg string) {
	shown = map[string]string{}
	for _, f := range files {
		for _, np := range []*string{&f.OldName, &f.NewName} {
			name := *np
			if name == "" || name == "/dev/null" {
				continue
			}
			abs, _, rootAbs, ok := tools.ResolveMaybe(t.root, name)
			if !ok {
				return nil, fmt.Sprintf("patch path %q escapes workspace root %q; refused (no files changed)", name, rootAbs)
			}
			target, denied := tools.CheckWriteTarget(t.root, abs, t.pathRe)
			if denied != "" {
				return nil, denied + " (no files changed)"
			}
			shown[target] = abs
			*np = target
		}
	}
	return shown, ""
}

// lineCounts totals the added and removed lines across a file's hunks.
func lineCounts(f *gitdiff.File) (added, removed int) {
	for _, frag := range f.TextFragments {
		added += int(frag.LinesAdded)
		removed += int(frag.LinesDeleted)
	}
	return added, removed
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
