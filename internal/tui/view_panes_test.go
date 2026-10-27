package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBoxRows_ClipsInsteadOfWrapping(t *testing.T) {
	long := strings.Repeat("word ", 30)
	out := boxRows("title\n"+long+"\nshort", 40)
	rows := strings.SplitSeq(out, "\n")
	for ln := range rows {
		if w := lipgloss.Width(ln); w != 40 {
			t.Fatalf("row is %d cells in a 40 cell box: %q", w, stripANSI(ln))
		}
	}
	if n := strings.Count(stripANSI(out), "word"); n >= 30 || !strings.Contains(stripANSI(out), glyphMore) {
		t.Fatalf("long row should clip with an ellipsis, kept %d words:\n%s", n, stripANSI(out))
	}
}

// every settings row stays one row at any terminal width: a wrapped
// description would start a line with no toggle and shear the columns
func TestSettingsPane_RowsNeverWrap(t *testing.T) {
	for _, w := range []int{60, 80, 100} {
		p := NewSettingsPane()
		p.Show(SettingsSnapshot{})
		out := stripANSI(p.View(w, 60))
		for ln := range strings.SplitSeq(out, "\n") {
			body := strings.TrimSpace(strings.Trim(ln, "│╭╮╰╯─ "))
			if body == "" || strings.HasPrefix(body, "⬢") || strings.HasPrefix(body, "›") ||
				strings.HasPrefix(body, "type filter") {
				continue
			}
			if !strings.HasPrefix(body, "[") && !strings.HasPrefix(body, glyphSel) {
				t.Fatalf("width %d: wrapped row %q in:\n%s", w, body, out)
			}
		}
	}
}
