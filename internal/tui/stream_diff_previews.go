package tui

import (
	"fmt"
	"strings"
)

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
		lines = append(lines, r.diffSign("+", expandTabs(l), path, r.styles.DiffAdd))
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
			lines = append(lines, r.diffSign("+", expandTabs(s), path, r.styles.DiffAdd))
		}
	}
	return header, r.diffBody(lines), true
}

// colorPatchLine picks the style for one raw unified-diff line. When
// highlighting is on, content after the +/- sign goes through chroma so
// the code itself reads as code; the sign keeps the green/red rail color.
// Tabs in the content are expanded to a fixed width so columns align under
// the rail instead of jumping to terminal-default tab stops.
func (r *StreamRenderer) colorPatchLine(l string) string {
	switch {
	case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"),
		strings.HasPrefix(l, "diff "), strings.HasPrefix(l, "@@"),
		strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file"),
		strings.HasPrefix(l, "deleted file"), strings.HasPrefix(l, "rename "),
		strings.HasPrefix(l, "similarity "):
		return r.styles.DiffMeta.Render(l)
	case strings.HasPrefix(l, "+"):
		body := expandTabs(strings.TrimPrefix(l, "+"))
		if r.highlight {
			return r.styles.DiffAdd.Render("+") + r.hlLang(body, "")
		}
		return r.styles.DiffAdd.Render("+" + body)
	case strings.HasPrefix(l, "-"):
		body := expandTabs(strings.TrimPrefix(l, "-"))
		if r.highlight {
			return r.styles.DiffDel.Render("-") + r.hlLang(body, "")
		}
		return r.styles.DiffDel.Render("-" + body)
	default:
		return r.styles.ToolPrev.Render(expandTabs(l))
	}
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
