// Package hashline_edit implements LINE#ID-anchored file edits.
//
// Each edit cites a position as "<lineNumber>#<3-char tag>" (e.g. "42#VKM").
// The tag is the content-hash of the line as it existed when the model saw
// it (via view with hashline=true). Before applying, every claimed tag is
// recomputed against the live file. If any tag is stale, the whole batch
// is rejected and the file is left untouched, no partial writes.
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
								"description": "Anchor in <lineNumber>#<3-char-tag> form.",
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
