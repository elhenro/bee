package tui

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// diffPreviewLinesCompact caps body height for compact-mode edit previews.
// Verbose mode lets every line through (mirrors previewLines() semantics).
// Sized to fit a focused hunk: 1 context + change block + 1 context.
const diffPreviewLinesCompact = 14

// diffContextLines is the per-side context retained around each change
// block when collapsing unchanged runs. Keep tight (1) so dense edits don't
// blow the cap; LCS already strips matched lines.
const diffContextLines = 1

// diffTabWidth expands tab characters in diff bodies to a fixed number of
// spaces. Tabs were rendering as terminal default tab stops (8 cols) inside
// a styled rail, which visually skewed indentation and pushed continuation
// content off the right edge of the card. 4 spaces matches the project's
// gofmt visual width and stays readable on narrow terminals.
const diffTabWidth = 4

// renderEditPreview turns a file-mutation tool call into a pretty
// `path` header + colored diff body. Returns (header, body, true) when u
// is one of: edit, hashline_edit, apply_patch, write. Body uses the lilac
// tool-rail so it groups visually under the call card; +/- lines get
// green/red. Returns ("", "", false) for every other tool so the caller
// can fall back to the plain json summary path.
func (r *StreamRenderer) renderEditPreview(u types.ToolUse) (string, string, bool) {
	switch u.Name {
	case "edit":
		return r.previewEdit(u.Input)
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
// reader sees only the actual changes — not the whole `old` payload followed
// by the whole `new` payload. Unchanged context is dimmed and collapses to a
// `⋯ +K unchanged` marker when long. Header carries `+adds −dels` stats so
// the change scale reads at a glance.
//
//	path  +3 −2  (occ 2)
//	▎   <context line>
//	▎ - <removed>
//	▎ + <added>
//	▎ ⋯ +12 unchanged
//	▎   <context line>
func (r *StreamRenderer) previewEdit(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	oldStr, _ := in["old"].(string)
	newStr, _ := in["new"].(string)
	if path == "" {
		return "", "", true
	}
	oldLines := splitKeepEmpty(oldStr)
	newLines := splitKeepEmpty(newStr)
	ops := lineDiff(oldLines, newLines)
	adds, dels := countDiffOps(ops)
	hunks := collapseToHunks(ops, diffContextLines)

	header := r.styles.DiffPath.Render(shortenPath(path))
	if occ, ok := numericField(in["occurrence"]); ok && occ != 1 {
		header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(occ %d)", occ))
	}
	header += "  " + r.diffStats(adds, dels)
	return header, r.diffBody(r.renderDiffOps(hunks, path)), true
}

// renderDiffOps turns post-collapse edit ops into styled rail lines. Context
// (`=`) is dimmed and signless so changes pop; gap markers ride DiffMeta.
// Trailing empty context (`=`-blank) is dropped — splitKeepEmpty preserves
// the empty entry after a trailing newline and there's no value in rendering
// an empty dimmed row at the end of every diff.
func (r *StreamRenderer) renderDiffOps(ops []editOp, path string) []string {
	for len(ops) > 0 {
		last := ops[len(ops)-1]
		if last.kind == '=' && last.text == "" {
			ops = ops[:len(ops)-1]
			continue
		}
		break
	}
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op.kind {
		case '+':
			out = append(out, r.diffSign("+", expandTabs(op.text), path, r.styles.DiffAdd))
		case '-':
			out = append(out, r.diffSign("-", expandTabs(op.text), path, r.styles.DiffDel))
		case '=':
			out = append(out, r.diffContext(expandTabs(op.text)))
		case '~':
			out = append(out, r.styles.DiffMeta.Render("⋯ "+op.text))
		}
	}
	return out
}

// diffContext styles an unchanged context line: 2-space gutter (aligns with
// `+ ` / `- `) and dimmed content so the eye skims past it to the changes.
func (r *StreamRenderer) diffContext(content string) string {
	return r.styles.Dim.Render("  " + content)
}

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

// diffBody wraps a slice of already-styled lines in the lilac tool rail
// (same as renderToolResult) and applies the compact-mode line cap. When
// every line would render empty the empty string is returned so the
// caller can omit the body entirely. When the cap fires, the body is
// trimmed by balancedTrim so both `-` and `+` rails stay represented —
// the previous head-cap could fully hide the additions when the removal
// block was long enough to fill the budget alone.
func (r *StreamRenderer) diffBody(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	hidden := 0
	if !r.verbose && len(lines) > diffPreviewLinesCompact {
		hidden = len(lines) - diffPreviewLinesCompact
		lines = balancedTrim(lines, diffPreviewLinesCompact)
	}
	rail := r.styles.ToolRail.Render("▎")
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = rail + " " + ln
	}
	body := strings.Join(out, "\n")
	if hidden > 0 {
		body += "\n" + rail + " " + r.styles.Dim.Render(fmt.Sprintf("⋯ +%d more", hidden))
	}
	return body
}

// splitKeepEmpty splits on \n preserving trailing empty entries. Avoids the
// strings.Split surprise where "" returns [""].
func splitKeepEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// expandTabs replaces tab characters with diffTabWidth spaces. Diff rows
// render under a styled rail and a 1-col `+`/`-` sign, so raw tabs land on
// terminal-default tab stops (8 cols) and jump content well past the rail's
// visual indent — making it look as if random characters were prepended to
// the first identifier. Expanding here keeps the visual column for each
// rendered line aligned with the rail.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", diffTabWidth))
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
