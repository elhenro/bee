package tui

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// diffPreviewLinesCompact caps body height for compact-mode edit previews.
// Verbose mode lets every line through (mirrors previewLines() semantics).
const diffPreviewLinesCompact = 8

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

// previewEdit renders the edit tool: replace Nth `old` with `new`.
//
//	path
//	▎ - <old line 1>
//	▎ + <new line 1>
func (r *StreamRenderer) previewEdit(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	old, _ := in["old"].(string)
	newStr, _ := in["new"].(string)
	if path == "" {
		return "", "", true
	}
	header := r.styles.DiffPath.Render(shortenPath(path))
	if occ, ok := numericField(in["occurrence"]); ok && occ != 1 {
		header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(occ %d)", occ))
	}
	var lines []string
	for _, l := range splitKeepEmpty(old) {
		lines = append(lines, r.diffSign("-", l, path, r.styles.DiffDel))
	}
	for _, l := range splitKeepEmpty(newStr) {
		lines = append(lines, r.diffSign("+", l, path, r.styles.DiffAdd))
	}
	return header, r.diffBody(lines), true
}

// previewWrite renders the write tool. Treats the whole content as added
// lines so the user sees what is being written — capped in compact mode.
func (r *StreamRenderer) previewWrite(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	content, _ := in["content"].(string)
	if path == "" {
		return "", "", true
	}
	header := r.styles.DiffPath.Render(shortenPath(path))
	all := splitKeepEmpty(content)
	header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(write, %d lines)", len(all)))
	lines := make([]string, 0, len(all))
	for _, l := range all {
		lines = append(lines, r.diffSign("+", l, path, r.styles.DiffAdd))
	}
	return header, r.diffBody(lines), true
}

// previewApplyPatch parses the unified diff in `patch` and renders each
// hunk colored. File headers (`diff --git`, `---`, `+++`, `@@`) stay
// dimmed-meta; +/- lines pick up the green/red diff styles.
func (r *StreamRenderer) previewApplyPatch(in map[string]any) (string, string, bool) {
	patch, _ := in["patch"].(string)
	if patch == "" {
		return "", "", true
	}
	files := patchFiles(patch)
	header := r.styles.DiffPath.Render(strings.Join(files, ", "))
	if len(files) == 0 {
		header = r.styles.DiffMeta.Render("(empty patch)")
	}
	var lines []string
	for _, raw := range splitKeepEmpty(strings.TrimRight(patch, "\n")) {
		lines = append(lines, r.colorPatchLine(raw))
	}
	return header, r.diffBody(lines), true
}

// previewHashlineEdit renders each anchored edit op as a styled block.
//
//	path  (3 edits)
//	▎ 42#VK replace
//	▎ + new line
func (r *StreamRenderer) previewHashlineEdit(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	edits, _ := in["edits"].([]any)
	if path == "" {
		return "", "", true
	}
	header := r.styles.DiffPath.Render(shortenPath(path))
	if dry, _ := in["dry_run"].(bool); dry {
		header += "  " + r.styles.DiffMeta.Render("(dry-run)")
	}
	header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(%d edit%s)", len(edits), pluralS(len(edits))))
	var lines []string
	for _, e := range edits {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		pos, _ := m["pos"].(string)
		op, _ := m["op"].(string)
		lines = append(lines, r.styles.DiffMeta.Render(fmt.Sprintf("%s %s", pos, op)))
		raw, _ := m["lines"].([]any)
		for _, l := range raw {
			s, _ := l.(string)
			lines = append(lines, r.diffSign("+", s, path, r.styles.DiffAdd))
		}
	}
	return header, r.diffBody(lines), true
}

// colorPatchLine picks the style for one raw unified-diff line. When
// highlighting is on, content after the +/- sign goes through chroma so
// the code itself reads as code; the sign keeps the green/red rail color.
func (r *StreamRenderer) colorPatchLine(l string) string {
	switch {
	case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"),
		strings.HasPrefix(l, "diff "), strings.HasPrefix(l, "@@"),
		strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file"),
		strings.HasPrefix(l, "deleted file"), strings.HasPrefix(l, "rename "),
		strings.HasPrefix(l, "similarity "):
		return r.styles.DiffMeta.Render(l)
	case strings.HasPrefix(l, "+"):
		if r.highlight {
			return r.styles.DiffAdd.Render("+") + r.hlLang(strings.TrimPrefix(l, "+"), "")
		}
		return r.styles.DiffAdd.Render(l)
	case strings.HasPrefix(l, "-"):
		if r.highlight {
			return r.styles.DiffDel.Render("-") + r.hlLang(strings.TrimPrefix(l, "-"), "")
		}
		return r.styles.DiffDel.Render(l)
	default:
		return r.styles.ToolPrev.Render(l)
	}
}

// diffBody wraps a slice of already-styled lines in the lilac tool rail
// (same as renderToolResult) and applies the compact-mode line cap. When
// every line would render empty the empty string is returned so the
// caller can omit the body entirely.
func (r *StreamRenderer) diffBody(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	hidden := 0
	if !r.verbose && len(lines) > diffPreviewLinesCompact {
		hidden = len(lines) - diffPreviewLinesCompact
		lines = lines[:diffPreviewLinesCompact]
	}
	rail := r.styles.ToolRail.Render("▎")
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = rail + " " + ln
	}
	body := strings.Join(out, "\n")
	if hidden > 0 {
		body += "\n" + rail + " " + r.styles.Dim.Render(fmt.Sprintf("… +%d more", hidden))
	}
	return body
}

// patchFiles returns the list of `+++ b/<path>` (or `--- a/<path>`)
// targets from a unified diff. Used only for the header label; ordering
// preserves the patch order.
func patchFiles(patch string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.Split(patch, "\n") {
		var p string
		switch {
		case strings.HasPrefix(l, "+++ b/"):
			p = strings.TrimPrefix(l, "+++ b/")
		case strings.HasPrefix(l, "+++ "):
			p = strings.TrimPrefix(l, "+++ ")
		case strings.HasPrefix(l, "--- b/") && len(out) == 0:
			p = strings.TrimPrefix(l, "--- b/")
		}
		if p == "" || p == "/dev/null" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// splitKeepEmpty splits on \n preserving trailing empty entries. Avoids the
// strings.Split surprise where "" returns [""].
func splitKeepEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
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
