// Package hashline_edit implements LINE#ID-anchored file edits.
//
// Each edit cites a position as "<lineNumber>#<2-char tag>" (e.g. "42#VK").
// The tag is the content-hash of the line as it existed when the model saw
// it (via view with hashline=true). Before applying, every claimed tag is
// recomputed against the live file. If any tag is stale, the whole batch
// is rejected and the file is left untouched — no partial writes.
//
// Edits are applied bottom-up (highest line number first) so each edit's
// line number stays valid even after earlier edits change line counts.
package hashline_edit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/apply_patch"
)

const toolName = "hashline_edit"

// Tool is the hashline_edit tool.
type Tool struct {
	pathRe *regexp.Regexp
}

// New returns a fresh hashline_edit tool.
func New() tools.Tool { return NewWithFilter(nil) }

// NewWithFilter constructs the hashline_edit tool with an optional path regex.
// When pathRe is nil, all paths are allowed (existing behavior).
// When pathRe is non-nil, edits to paths that do NOT match are rejected with
// a clear error and the file is left untouched. Paths are matched relative
// to the current working directory when possible.
func NewWithFilter(pathRe *regexp.Regexp) tools.Tool {
	return &Tool{pathRe: pathRe}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Edit a file using LINE#ID anchors (e.g. 42#VK). Get anchors from read with hashline=true. " +
			"Edits are validated against live content and rejected on mismatch — no stale-line corruption. " +
			"Result echoes fresh anchors around each edit so a follow-up edit needs no re-read. " +
			"Set dry_run=true to preview without writing.",
		PromptSnippet: "Hash-anchored line edit (no line numbers)",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to edit (relative or absolute).",
				},
				"edits": map[string]any{
					"type":        "array",
					"description": "List of edits, each referencing the original snapshot.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"pos": map[string]any{
								"type":        "string",
								"description": "Anchor in <lineNumber>#<2-char-tag> form.",
							},
							"op": map[string]any{
								"type":        "string",
								"description": "One of: replace, prepend, append.",
								"enum":        []string{"replace", "prepend", "append"},
							},
							"lines": map[string]any{
								"type":        "array",
								"description": "Replacement or insertion lines (no trailing newlines).",
								"items":       map[string]any{"type": "string"},
							},
						},
						"required": []string{"pos", "op", "lines"},
					},
				},
				"dry_run": map[string]any{
					"type":        "boolean",
					"description": "When true, validate + compute new anchors but do NOT write. Result still echoes the would-be state around each edit so you can verify intent before committing.",
				},
			},
			"required": []string{"path", "edits"},
		},
	}
}

// edit is one parsed mutation.
type edit struct {
	rawPos  string
	line    int // 1-based
	tag     string
	op      string
	lines   []string
}

// Run validates and applies the batch atomically.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	path, _ := in["path"].(string)
	if path == "" {
		return tools.Result{Content: "missing path", IsError: true}, nil
	}
	rawEdits, ok := in["edits"].([]any)
	if !ok || len(rawEdits) == 0 {
		return tools.Result{Content: "missing or empty edits", IsError: true}, nil
	}
	dryRun, _ := in["dry_run"].(bool)

	if t.pathRe != nil {
		match := path
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, path); err == nil {
				match = rel
			}
		}
		if !t.pathRe.MatchString(match) {
			return tools.Result{Content: fmt.Sprintf("path %q denied by write filter", match), IsError: true}, nil
		}
	}

	edits, err := parseEdits(rawEdits)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	// preserve trailing newline behavior: split keeps the empty tail entry.
	hadTrailing := strings.HasSuffix(string(data), "\n")
	body := string(data)
	if hadTrailing {
		body = strings.TrimSuffix(body, "\n")
	}
	lines := strings.Split(body, "\n")
	// special case: empty file → no lines to anchor against.
	if body == "" {
		lines = nil
	}

	if err := validateEdits(edits, lines); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}

	// compute each edit's post-apply line range BEFORE mutating, walking in
	// ascending-original-line order with a running shift. Each edit shifts
	// lines below it by `delta` (net add/remove). Edits above are unaffected.
	type span struct {
		origLine int    // pre-edit 1-based line for labelling
		newFrom  int    // post-edit 1-based first line of the edit's content
		newTo    int    // post-edit 1-based last line; < newFrom means deletion
		op       string
	}
	ascending := make([]edit, len(edits))
	copy(ascending, edits)
	sort.SliceStable(ascending, func(i, j int) bool { return ascending[i].line < ascending[j].line })
	spans := make([]span, 0, len(ascending))
	shift := 0
	for _, e := range ascending {
		newLine := e.line + shift
		var newFrom, newTo, delta int
		switch e.op {
		case "replace":
			newFrom = newLine
			newTo = newLine + len(e.lines) - 1
			delta = len(e.lines) - 1 // -1 for the line we removed
		case "prepend":
			newFrom = newLine
			newTo = newLine + len(e.lines) - 1
			delta = len(e.lines)
		case "append":
			newFrom = newLine + 1
			newTo = newLine + len(e.lines)
			delta = len(e.lines)
		}
		spans = append(spans, span{origLine: e.line, newFrom: newFrom, newTo: newTo, op: e.op})
		shift += delta
	}

	// bottom-up: highest line number first so earlier indices stay valid.
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].line > edits[j].line })
	for _, e := range edits {
		lines = applyOne(lines, e)
	}

	out := strings.Join(lines, "\n")
	if hadTrailing || (len(lines) > 0 && out != "") {
		// keep original trailing-newline convention; if we inserted into
		// an empty file we still want a final newline for sanity.
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
	}

	if !dryRun {
		if err := os.WriteFile(path, []byte(out), info.Mode().Perm()); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
	}

	// echo: for each edit emit fresh anchors covering the new range + 2 lines
	// of context, so a follow-up edit doesn't need a re-read.
	var b strings.Builder
	verb := "applied"
	if dryRun {
		verb = "dry-run"
	}
	fmt.Fprintf(&b, "%s %d edit(s) to %s\n", verb, len(edits), path)
	const ctxLines = 2
	for _, s := range spans {
		// deletion (newTo < newFrom): anchor surrounding lines instead.
		anchorFrom := s.newFrom
		anchorTo := s.newTo
		if anchorTo < anchorFrom {
			anchorFrom = s.newFrom - 1
			anchorTo = s.newFrom
		}
		from := anchorFrom - ctxLines
		if from < 1 {
			from = 1
		}
		to := anchorTo + ctxLines
		if to > len(lines) {
			to = len(lines)
		}
		fmt.Fprintf(&b, "\n# %s @ orig line %d → new lines %d-%d\n", s.op, s.origLine, s.newFrom, s.newTo)
		for ln := from; ln <= to; ln++ {
			fmt.Fprintf(&b, "%6d#%s │ %s\n", ln, apply_patch.Tag(lines[ln-1], ln), lines[ln-1])
		}
	}
	return tools.Result{Content: strings.TrimRight(b.String(), "\n")}, nil
}

func parseEdits(raw []any) ([]edit, error) {
	out := make([]edit, 0, len(raw))
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edit %d: not an object", i)
		}
		pos, _ := m["pos"].(string)
		op, _ := m["op"].(string)
		linesAny, _ := m["lines"].([]any)

		ln, tag, err := parsePos(pos)
		if err != nil {
			return nil, err
		}
		if op != "replace" && op != "prepend" && op != "append" {
			return nil, fmt.Errorf("bad op %q", op)
		}
		lines := make([]string, 0, len(linesAny))
		for j, l := range linesAny {
			s, ok := l.(string)
			if !ok {
				return nil, fmt.Errorf("edit %d line %d: not a string", i, j)
			}
			lines = append(lines, s)
		}
		out = append(out, edit{rawPos: pos, line: ln, tag: tag, op: op, lines: lines})
	}
	return out, nil
}

func parsePos(pos string) (int, string, error) {
	hash := strings.IndexByte(pos, '#')
	if hash <= 0 || hash != len(pos)-3 {
		return 0, "", fmt.Errorf("bad pos %q; want <lineNumber>#<2-char-tag>", pos)
	}
	ln, err := strconv.Atoi(pos[:hash])
	if err != nil || ln < 1 {
		return 0, "", fmt.Errorf("bad pos %q; want <lineNumber>#<2-char-tag>", pos)
	}
	tag := pos[hash+1:]
	if len(tag) != 2 {
		return 0, "", fmt.Errorf("bad pos %q; want <lineNumber>#<2-char-tag>", pos)
	}
	return ln, tag, nil
}

func validateEdits(edits []edit, lines []string) error {
	seen := make(map[int]bool, len(edits))
	for _, e := range edits {
		if seen[e.line] {
			return fmt.Errorf("overlapping edit at pos %s", e.rawPos)
		}
		seen[e.line] = true
		if e.line > len(lines) {
			return fmt.Errorf("line %d out of range; file has %d lines", e.line, len(lines))
		}
		live := lines[e.line-1]
		got := apply_patch.Tag(live, e.line)
		if got != e.tag {
			return fmt.Errorf(
				"hash mismatch at line %d: claimed %s, live %s\n  - expected: %s\n  + actual:   %s",
				e.line, e.tag, got, e.tag, got,
			)
		}
	}
	return nil
}

// applyOne mutates lines per edit. Caller must apply bottom-up so indices
// stay valid across the batch.
func applyOne(lines []string, e edit) []string {
	idx := e.line - 1
	switch e.op {
	case "replace":
		// splice: drop the target line, insert replacement lines in its place.
		head := append([]string{}, lines[:idx]...)
		tail := append([]string{}, lines[idx+1:]...)
		return append(append(head, e.lines...), tail...)
	case "prepend":
		head := append([]string{}, lines[:idx]...)
		tail := append([]string{}, lines[idx:]...)
		return append(append(head, e.lines...), tail...)
	case "append":
		head := append([]string{}, lines[:idx+1]...)
		tail := append([]string{}, lines[idx+1:]...)
		return append(append(head, e.lines...), tail...)
	}
	return lines
}
