package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestOnChangeMapRowsFitNarrowTerminal(t *testing.T) {
	st := DefaultStyles()
	sb := &scrollback{}
	m := Model{width: 30, styles: &st, scroll: sb}
	card := "changes · 1 file\n  internal/very/long/path/to/some/file_name.go  +10 −2  · because reasons"
	if _, cmd := m.onChangeMap(changeMapMsg{text: card}); cmd == nil {
		t.Fatal("want a print cmd")
	}
	for _, blk := range sb.queue {
		for _, row := range strings.Split(blk, "\n") {
			if w := lipgloss.Width(row); w > 30 {
				t.Fatalf("row %d cols wide: %q", w, row)
			}
		}
	}
	if _, cmd := m.onChangeMap(changeMapMsg{}); cmd != nil {
		t.Fatal("empty card must print nothing")
	}
}
