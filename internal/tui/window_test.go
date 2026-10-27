package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/types"
)

func TestWindowRange(t *testing.T) {
	cases := []struct{ n, sel, max, start, end int }{
		{5, 4, 10, 0, 5},
		{40, 0, 10, 0, 10},
		{40, 20, 10, 15, 25},
		{40, 39, 10, 30, 40},
		{40, 5, 1, 4, 7}, // floor of 3 rows
	}
	for _, c := range cases {
		if got := windowRange(c.n, c.sel, c.max); got.start != c.start || got.end != c.end {
			t.Errorf("windowRange(%d,%d,%d) = %+v, want [%d,%d)", c.n, c.sel, c.max, got, c.start, c.end)
		}
	}
}

// a pane taller than the terminal loses its top rows (title, first entries
// and the cursor with them): long lists must window to the height and keep
// the selected row on screen.
func TestTallPanes_FitHeightAndKeepSelectionVisible(t *testing.T) {
	const w = 100
	mark := strings.TrimSpace(glyphSel)
	check := func(name string, h int, view string) {
		t.Helper()
		rows := strings.Split(stripANSI(view), "\n")
		if len(rows) > h {
			t.Errorf("%s h=%d: %d rows", name, h, len(rows))
		}
		sel := 0
		for _, r := range rows {
			if strings.Contains(r, mark) {
				sel++
			}
			if lipgloss.Width(r) > w {
				t.Errorf("%s h=%d: row of %d cells", name, h, lipgloss.Width(r))
			}
		}
		if sel != 1 {
			t.Errorf("%s h=%d: %d selected rows visible, want 1", name, h, sel)
		}
	}
	for _, h := range []int{14, 24} {
		for _, pos := range []int{0, 17, 39} {
			tp := NewToolsPane()
			var rows []commands.ToolInfo
			for i := range 40 {
				rows = append(rows, commands.ToolInfo{Name: fmt.Sprintf("tool_%02d", i), Description: "desc"})
			}
			tp.Show(rows)
			tp.cursor = pos
			check("tools", h, tp.View(w, h))

			sp := NewSettingsPane()
			sp.Show(SettingsSnapshot{})
			sp.cursor = min(pos, len(sp.matches)-1)
			check("settings", h, sp.View(w, h))

			st := NewSessionTree()
			// siblings under one root, so indentation stays shallow
			var msgs []types.Message
			for i := range 40 {
				parent := "m00"
				if i == 0 {
					parent = ""
				}
				msgs = append(msgs, types.Message{ID: fmt.Sprintf("m%02d", i), ParentID: parent, Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hello"}}})
			}
			st.LoadMessages(msgs, "m39")
			st.open = true
			st.selected = fmt.Sprintf("m%02d", pos)
			check("tree", h, st.View(w, h))
		}
	}
}
