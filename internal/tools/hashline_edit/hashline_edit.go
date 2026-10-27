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
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/apply_patch"
)

const toolName = "hashline_edit"

// Tool is the hashline_edit tool.
type Tool struct {
	root   string
	pathRe *regexp.Regexp
}

// NewWithFilter constructs the hashline_edit tool with an optional path regex.
// When pathRe is nil, all in-root paths are allowed. When non-nil, edits to
// paths that do NOT match are rejected and the file is left untouched. Either
// way, paths that escape root are refused.
func NewWithFilter(root string, pathRe *regexp.Regexp) tools.Tool {
	return &Tool{root: root, pathRe: pathRe}
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
					"minLength":   1,
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
	rawPos string
	line   int // 1-based
	tag    string
	op     string
	lines  []string
}

// Run validates and applies the batch atomically.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	path, _ := in["path"].(string)
	if path == "" {
		return tools.Result{Content: "missing path", IsError: true}, nil
	}
	// contain edits to the workspace root (symlink-resolved) under a confined
	// scope; reject absolute or ../ paths that escape it. In danger-full-access
	// (empty root) ResolveMaybe is a passthrough — edit anywhere.
	abs, _, rootAbs, ok := tools.ResolveMaybe(t.root, path)
	if !ok {
		return tools.Result{Content: fmt.Sprintf("path %q escapes workspace root %q", path, rootAbs), IsError: true}, nil
	}
	path = abs
	rawEdits, ok := in["edits"].([]any)
	if !ok || len(rawEdits) == 0 {
		return tools.Result{Content: "missing or empty edits", IsError: true}, nil
	}
	dryRun, _ := in["dry_run"].(bool)

	path, msg := tools.CheckWriteTarget(t.root, path, t.pathRe)
	if msg != "" {
		return tools.Result{Content: msg, IsError: true}, nil
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
	lines, eol := splitLines(string(data))

	if err := validateEdits(edits, lines); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}

	spans := editSpans(edits)

	// bottom-up: highest line number first so earlier indices stay valid.
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].line > edits[j].line })
	for _, e := range edits {
		lines = applyOne(lines, e)
	}

	out := strings.Join(lines, eol)
	// keep the original trailing-newline convention; an empty file that gained
	// content still ends in a newline.
	if (len(data) == 0 || data[len(data)-1] == '\n') && out != "" && !strings.HasSuffix(out, "\n") {
		out += eol
	}

	if !dryRun {
		if err := os.WriteFile(path, []byte(out), info.Mode().Perm()); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
	}

	verb := "applied"
	if dryRun {
		verb = "dry-run"
	}
	return tools.Result{Content: fmt.Sprintf("%s %d edit(s) to %s\n%s", verb, len(edits), path, echo(spans, lines))}, nil
}

// splitLines splits file content into lines plus the line ending to join them
// back with. A trailing newline yields no empty tail entry and an empty file no
// lines. Dominant CRLF files edit as plain lines and are written back with CRLF,
// so an edit never leaves bare LF endings in the file.
func splitLines(text string) (lines []string, eol string) {
	body := strings.TrimSuffix(text, "\n")
	eol = "\n"
	crlf := strings.Count(text, "\r\n")*2 > strings.Count(text, "\n")
	if crlf {
		eol = "\r\n"
	}
	if body == "" {
		return nil, eol
	}
	lines = strings.Split(body, "\n")
	if crlf {
		for i, l := range lines {
			lines[i] = strings.TrimSuffix(l, "\r")
		}
	}
	return lines, eol
}

// span is where one edit lands after the batch applies.
type span struct {
	origLine int // pre-edit 1-based line for labelling
	newFrom  int // post-edit 1-based first line of the edit's content
	newTo    int // post-edit 1-based last line; < newFrom means deletion
	op       string
}

// editSpans computes each edit's post-apply line range BEFORE mutating,
// walking in ascending-original-line order with a running shift. Each edit
// shifts lines below it by its net add/remove; edits above are unaffected.
func editSpans(edits []edit) []span {
	ascending := slices.Clone(edits)
	sort.SliceStable(ascending, func(i, j int) bool { return ascending[i].line < ascending[j].line })
	spans := make([]span, 0, len(ascending))
	shift := 0
	for _, e := range ascending {
		newFrom, delta := e.line+shift, len(e.lines)
		switch e.op {
		case "replace":
			delta-- // the replaced line is removed
		case "append":
			newFrom++
		}
		spans = append(spans, span{origLine: e.line, newFrom: newFrom, newTo: newFrom + len(e.lines) - 1, op: e.op})
		shift += delta
	}
	return spans
}

// echo emits fresh anchors covering each edit's new range + 2 lines of
// context, so a follow-up edit doesn't need a re-read.
func echo(spans []span, lines []string) string {
	var b strings.Builder
	const ctxLines = 2
	for _, s := range spans {
		// deletion (newTo < newFrom): anchor surrounding lines instead.
		anchorFrom, anchorTo := s.newFrom, s.newTo
		if anchorTo < anchorFrom {
			anchorFrom, anchorTo = s.newFrom-1, s.newFrom
		}
		from := max(anchorFrom-ctxLines, 1)
		to := min(anchorTo+ctxLines, len(lines))
		fmt.Fprintf(&b, "\n# %s @ orig line %d → new lines %d-%d\n", s.op, s.origLine, s.newFrom, s.newTo)
		for ln := from; ln <= to; ln++ {
			fmt.Fprintf(&b, "%6d#%s │ %s\n", ln, apply_patch.Tag(lines[ln-1], ln), lines[ln-1])
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
