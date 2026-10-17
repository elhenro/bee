package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/types"
)

// diffPreviewLinesCompact caps body height for compact-mode edit previews.
// Verbose mode lets every line through (mirrors previewRows semantics).
// Sized to fit a focused hunk: 1 context + change block + 1 context.
const diffPreviewLinesCompact = 14

// diffContextLines is the per-side context retained around each change
// block when collapsing unchanged runs. Keep tight (1) so dense edits don't
// blow the cap; LCS already strips matched lines.
const diffContextLines = 1

// diffTabWidth is the tab stop expandTabs aligns to. Terminal default stops
// (8 cols) pushed indentation off the right edge of a card; 4 matches gofmt's
// visual width and stays readable on narrow terminals.
const diffTabWidth = 4

// renderEditPreview turns a file-mutation tool call into a `path` header +
// colored diff body. Returns (header, body, true) when u is one of: edit,
// hashline_edit, apply_patch, write; changed rows get a green/red wash.
// Returns ("", "", false) for every other tool so the caller can fall back to
// the plain args summary. anchor is the file line an edit landed on, 0 when
// unknown (numbers are then left off).
func (r *StreamRenderer) renderEditPreview(u types.ToolUse, anchor int) (string, string, bool) {
	switch u.Name {
	case "edit":
		return r.previewEdit(u.Input, anchor)
	case "hashline_edit":
		return r.previewHashlineEdit(u.Input)
	case "apply_patch":
		return r.previewApplyPatch(u.Input)
	case "write":
		return r.previewWrite(u.Input)
	}
	return "", "", false
}

// previewEdit renders the edit tool as a real LCS-based line diff so the
// reader sees only the actual changes, not the whole `old` payload followed
// by the whole `new` payload. Unchanged context is dimmed and collapses to a
// `⋯ +K unchanged` marker when long. Header carries `+adds −dels` stats so
// the change scale reads at a glance.
//
//	path  +3 −2  (occ 2)
//	17   <context line>
//	18 - <removed>
//	18 + <added>
func (r *StreamRenderer) previewEdit(in map[string]any, anchor int) (string, string, bool) {
	path, _ := in["path"].(string)
	oldStr, _ := in["old"].(string)
	newStr, _ := in["new"].(string)
	if path == "" {
		return "", "", true
	}
	ops := lineDiff(splitKeepEmpty(oldStr), splitKeepEmpty(newStr))
	adds, dels := countDiffOps(ops)
	if anchor > 0 {
		numberOps(ops, anchor)
	}
	rows := layoutRows(collapseToHunks(ops, diffContextLines))

	header := r.styles.DiffPath.Render(shortenPath(path))
	if occ, ok := numericField(in["occurrence"]); ok && occ != 1 {
		header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(occ %d)", occ))
	}
	header += "  " + r.diffStats(adds, dels)
	return header, r.diffCard([]diffBlock{{path: path, rows: rows}}), true
}

// the file line an edit landed on, read off the edit tool's own echo
// (`# region 1 → lines 17-19`); 0 when the call failed or the echo is gone
func editAnchor(use types.ToolUse, res types.ToolResult) int {
	if use.Name != "edit" || res.IsError {
		return 0
	}
	if m := editRegion.FindStringSubmatch(res.Content); m != nil {
		return atoi(m[1])
	}
	return 0
}

var editRegion = regexp.MustCompile(`# region 1 → lines (\d+)`)

// diffStats renders the `+N −M` header badge with semantic colors. Symmetric
// minus uses U+2212 (true minus) not hyphen so it visually balances the `+`.
func (r *StreamRenderer) diffStats(adds, dels int) string {
	parts := make([]string, 0, 2)
	if adds > 0 {
		parts = append(parts, r.styles.DiffAdd.Render(fmt.Sprintf("+%d", adds)))
	}
	if dels > 0 {
		parts = append(parts, r.styles.DiffDel.Render(fmt.Sprintf("−%d", dels)))
	}
	if len(parts) == 0 {
		return r.styles.DiffMeta.Render("(no change)")
	}
	return strings.Join(parts, " ")
}

// splitKeepEmpty splits on \n preserving trailing empty entries. Avoids the
// strings.Split surprise where "" returns [""].
func splitKeepEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// expandTabs replaces each tab with spaces up to the next stop, diffTabWidth
// wide, counting from the start of its line. A fixed run of spaces would skew
// columns an output lined up with tabs (ls, go test); a raw tab is worse, it
// skips cells without erasing them, so a redrawn row keeps stale characters.
// Escape sequences take no width and a newline restarts the column.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	var state byte
	col := 0
	for len(s) > 0 {
		seq, w, n, next := ansi.DecodeSequence(s, state, nil)
		state = next
		switch {
		case seq == "\t":
			pad := diffTabWidth - col%diffTabWidth
			b.WriteString("    "[:pad])
			col += pad
		case strings.ContainsAny(seq, "\r\n"):
			b.WriteString(seq)
			col = 0
		default:
			b.WriteString(seq)
			col += w
		}
		s = s[n:]
	}
	return b.String()
}

// numericField coerces a JSON number (float64 from encoding/json) or int into int.
func numericField(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	}
	return 0, false
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
