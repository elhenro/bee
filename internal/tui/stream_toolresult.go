package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/types"
)

// compactLines splits clean output and drops blank/whitespace-only lines so
// previews don't waste rows on padding. Order is preserved. Non-printable
// control bytes (NUL, BS, BEL, raw CR — anything below 0x20 except tab) are
// replaced with `·` per line: grep accidentally hitting a binary blob,
// curl spitting raw bytes, etc. would otherwise blit blank rows + scrambled
// glyphs into the terminal even after ansi.Strip.
func compactLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = sanitizeControl(l)
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return out
}

// sanitizeControl drops every byte < 0x20 (except tab) and 0x7f. ansi.Strip
// handles CSI/OSC sequences; this catches the bare control bytes (NUL, CR,
// BS, BEL, ...) that wreck terminal layout when raw bytes leak in. Dropping
// (not substituting) lets a line of pure NULs collapse to empty so compactLines
// then discards it — no rows wasted on what was binary garbage.
func sanitizeControl(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || (r >= 0x20 && r != 0x7f) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var searchHit = regexp.MustCompile(`^(\S+?:\d+:)(.*)$`)

// `path:line:` stays subtle, the matched text steps up to the secondary tone
func (r *StreamRenderer) searchRow(ln string) string {
	m := searchHit.FindStringSubmatch(ln)
	if m == nil {
		return r.styles.ToolPrev.Render(ln)
	}
	return r.styles.ToolPrev.Render(m[1]) + lipgloss.NewStyle().Foreground(fgSmoke).Render(m[2])
}

// the one-row digest a file read collapses to
func readSummary(lines, bytes int) string {
	s := fmt.Sprintf("%d lines", lines)
	if lines == 1 {
		s = "1 line"
	}
	if bytes >= 1000 {
		s += " " + glyphSep + " " + humanBytes(bytes)
	}
	return s
}

// per-row highlighter for verbose reads (nil otherwise), lexer from the path
func (r *StreamRenderer) readHighlighter(use types.ToolUse, haveUse bool, res types.ToolResult) func(string) string {
	if !r.verbose || !r.highlight || res.IsError || !haveUse || use.Name != "read" {
		return nil
	}
	path, _ := use.Input["path"].(string)
	if path == "" {
		return nil
	}
	return func(s string) string { return r.hl(s, path) }
}

// file-mutation tools whose call card carries a diff
func isEditTool(name string) bool {
	switch name {
	case "edit", "hashline_edit", "apply_patch", "write":
		return true
	}
	return false
}
