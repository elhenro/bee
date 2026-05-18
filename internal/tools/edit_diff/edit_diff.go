// Package edit_diff implements the edit_diff tool: literal find/replace.
// One occurrence, all occurrences, or an expected-count guard for safety.
// Result echoes anchored lines around each affected region so a follow-up
// edit needs no re-read.
package edit_diff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/apply_patch"
)

const (
	toolName    = "edit"
	ctxLines    = 2
	maxEchoRuns = 5
)

// Tool is the edit_diff tool.
type Tool struct {
	root   string
	pathRe *regexp.Regexp
}

// New returns an edit_diff tool rooted at root. Root is currently informational —
// callers pass absolute paths.
func New(root string) *Tool { return NewWithFilter(root, nil) }

// NewWithFilter constructs the edit_diff tool with an optional path regex.
// When pathRe is nil, all paths are allowed. When non-nil, edits to paths
// (relative to root when possible) that do NOT match are rejected before
// the file is touched.
func NewWithFilter(root string, pathRe *regexp.Regexp) *Tool {
	return &Tool{root: root, pathRe: pathRe}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Replace literal 'old' with 'new' in a file. " +
			"Default replaces the 1st occurrence; set occurrence=N for the Nth, " +
			"or replace_all=true for every match. Set count=K to refuse unless " +
			"exactly K occurrences exist (catches stale assumptions). Result echoes " +
			"hashline anchors around each affected region so chained edits skip a re-read.",
		PromptSnippet: "literal find/replace (Nth or all) with anchored echo",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string"},
				"old":         map[string]any{"type": "string"},
				"new":         map[string]any{"type": "string"},
				"occurrence":  map[string]any{"type": "integer", "description": "1-based; default 1. Ignored when replace_all=true."},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence."},
				"count":       map[string]any{"type": "integer", "description": "Expected number of occurrences. Refuses to edit if actual count differs."},
			},
			"required": []string{"path", "old", "new"},
		},
	}
}

// Run performs the edit.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	path, _ := in["path"].(string)
	if path == "" {
		return tools.Result{Content: "missing path", IsError: true}, nil
	}
	old, ok := in["old"].(string)
	if !ok || old == "" {
		return tools.Result{Content: "missing old", IsError: true}, nil
	}
	newStr, ok := in["new"].(string)
	if !ok || newStr == "" {
		return tools.Result{Content: "new must be non-empty; use a separate tool for deletion", IsError: true}, nil
	}
	occ := tools.IntArg(in, "occurrence", 1)
	if occ < 1 {
		return tools.Result{Content: "occurrence must be >= 1", IsError: true}, nil
	}
	expected := tools.IntArg(in, "count", 0)
	replaceAll, _ := in["replace_all"].(bool)

	if t.pathRe != nil {
		match := path
		if rel, err := filepath.Rel(t.root, path); err == nil {
			match = rel
		}
		if !t.pathRe.MatchString(match) {
			return tools.Result{Content: fmt.Sprintf("path %q denied by write filter", match), IsError: true}, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	src := string(data)
	hits := allIndex(src, old)
	count := len(hits)
	if count == 0 {
		return tools.Result{Content: fmt.Sprintf("old not found in %s", path), IsError: true}, nil
	}
	if expected > 0 && expected != count {
		return tools.Result{Content: fmt.Sprintf("count guard: expected %d occurrence(s) of old in %s, found %d", expected, path, count), IsError: true}, nil
	}
	if !replaceAll && occ > count {
		return tools.Result{Content: fmt.Sprintf("occurrence %d > count %d in %s", occ, count, path), IsError: true}, nil
	}

	var targets []int
	if replaceAll {
		targets = hits
	} else {
		targets = []int{hits[occ-1]}
	}

	out, spans := splice(src, targets, old, newStr)

	info, err := os.Stat(path)
	mode := os.FileMode(0o644)
	if err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(out), mode); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}

	return tools.Result{Content: echo(path, out, spans, len(targets), count)}, nil
}

// allIndex returns the byte offsets of every non-overlapping match of sub
// in s, scanning left-to-right.
func allIndex(s, sub string) []int {
	var out []int
	from := 0
	for {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			return out
		}
		out = append(out, from+i)
		from += i + len(sub)
	}
}

// splice replaces each occurrence at the given byte offsets (in src) with
// newStr and returns the new content plus output-side byte spans of the
// inserted text.
func splice(src string, starts []int, old, newStr string) (string, [][2]int) {
	var b strings.Builder
	spans := make([][2]int, 0, len(starts))
	prev := 0
	for _, idx := range starts {
		b.WriteString(src[prev:idx])
		s := b.Len()
		b.WriteString(newStr)
		spans = append(spans, [2]int{s, b.Len()})
		prev = idx + len(old)
	}
	b.WriteString(src[prev:])
	return b.String(), spans
}

// echo renders a one-line summary plus anchored context windows around each
// affected region. Capped at maxEchoRuns to keep output bounded.
func echo(path, out string, spans [][2]int, replaced, total int) string {
	var b strings.Builder
	if replaced == total {
		fmt.Fprintf(&b, "replaced %d occurrence(s) in %s\n", replaced, path)
	} else {
		fmt.Fprintf(&b, "replaced %d of %d occurrence(s) in %s\n", replaced, total, path)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	shown := 0
	for i, sp := range spans {
		if shown >= maxEchoRuns {
			fmt.Fprintf(&b, "\n# ... %d more region(s) elided\n", len(spans)-shown)
			break
		}
		startLine := 1 + strings.Count(out[:sp[0]], "\n")
		endLine := 1 + strings.Count(out[:sp[1]], "\n")
		if endLine > len(lines) {
			endLine = len(lines)
		}
		from := startLine - ctxLines
		if from < 1 {
			from = 1
		}
		to := endLine + ctxLines
		if to > len(lines) {
			to = len(lines)
		}
		fmt.Fprintf(&b, "\n# region %d → lines %d-%d\n", i+1, startLine, endLine)
		for ln := from; ln <= to; ln++ {
			fmt.Fprintf(&b, "%6d#%s │ %s\n", ln, apply_patch.Tag(lines[ln-1], ln), lines[ln-1])
		}
		shown++
	}
	return strings.TrimRight(b.String(), "\n")
}
