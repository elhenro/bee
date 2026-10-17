package tui

import (
	"fmt"
	"strings"
)

// previewWrite renders the write tool: every line is an addition, numbered
// from 1, capped like any other diff
func (r *StreamRenderer) previewWrite(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	content, _ := in["content"].(string)
	if path == "" {
		return "", "", true
	}
	lines := splitKeepEmpty(strings.TrimSuffix(content, "\n"))
	ops := make([]editOp, len(lines))
	for i, l := range lines {
		ops[i] = editOp{kind: '+', text: l, new: i + 1}
	}
	header := r.styles.DiffPath.Render(shortenPath(path)) + "  " + r.diffStats(len(lines), 0)
	return header, r.diffCard([]diffBlock{{path: path, rows: layoutRows(ops)}}), true
}

// previewApplyPatch renders a unified diff the way an edit renders: a header
// with the file and its +/- stats, then numbered rows with a hunk's worth of
// context. file headers and `@@` lines are folded into that, not echoed
func (r *StreamRenderer) previewApplyPatch(in map[string]any) (string, string, bool) {
	patch, _ := in["patch"].(string)
	if patch == "" {
		return "", "", true
	}
	files := parsePatch(patch)
	if len(files) == 0 {
		return r.styles.DiffMeta.Render("(empty patch)"), "", true
	}
	adds, dels := 0, 0
	blocks := make([]diffBlock, len(files))
	for i, f := range files {
		adds += f.adds
		dels += f.dels
		blocks[i] = diffBlock{path: f.path, rows: layoutRows(collapseToHunks(f.ops, diffContextLines))}
		if len(files) > 1 {
			blocks[i].label = r.fileTitle(f) + "  " + r.diffStats(f.adds, f.dels)
		}
	}
	header := r.fileTitle(files[0])
	if len(files) > 1 {
		header = r.styles.DiffPath.Render(fmt.Sprintf("%d files", len(files)))
	}
	return header + "  " + r.diffStats(adds, dels), r.diffCard(blocks), true
}

// path plus a (new)/(deleted) badge
func (r *StreamRenderer) fileTitle(f patchFile) string {
	t := r.styles.DiffPath.Render(shortenPath(f.path))
	if f.state != "" {
		t += " " + r.styles.DiffMeta.Render("("+f.state+")")
	}
	return t
}

// previewHashlineEdit renders each anchored edit op as a meta row plus its
// new lines.
//
//	path  (3 edits)
//	  42#VK replace
//	+ new line
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
	var rows []diffRow
	for _, e := range edits {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		pos, _ := m["pos"].(string)
		op, _ := m["op"].(string)
		rows = append(rows, diffRow{kind: 'm', text: pos + " " + op})
		raw, _ := m["lines"].([]any)
		for _, l := range raw {
			s, _ := l.(string)
			rows = append(rows, diffRow{kind: '+', text: s})
		}
	}
	return header, r.diffCard([]diffBlock{{path: path, rows: rows}}), true
}
