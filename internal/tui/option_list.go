package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// optionList is the focus and key handling shared by the inline option
// pickers under the live region (escalate, post-scout): up/down wrap, number
// keys pick, esc deactivates. The embedding picker owns the options.
type optionList struct {
	styles *Styles
	Active bool
	focus  int
	width  int
}

func (l optionList) isActive() bool { return l.Active }

// SetWidth records the terminal width so View can wrap option rows.
func (l *optionList) SetWidth(w int) { l.width = w }

// key applies a key press to a list of n options and returns the index picked
// by enter or a number key, else -1. Esc deactivates the list.
func (l *optionList) key(msg tea.Msg, n int) int {
	km, ok := msg.(tea.KeyMsg)
	if !l.Active || !ok {
		return -1
	}
	switch s := km.String(); s {
	case "up", "k", "shift+tab":
		l.focus = (l.focus + n - 1) % n
	case "down", "j", "tab":
		l.focus = (l.focus + 1) % n
	case "esc":
		l.Active = false
	case "enter":
		return l.focus
	default:
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' && int(s[0]-'1') < n {
			return int(s[0] - '1')
		}
	}
	return -1
}

// render draws the optional head line, the numbered options (the focused one
// highlighted) and the hint, all tied to the warn rail.
func (l optionList) render(head string, labels []string, hint string) string {
	rail := l.styles.WarnRail.Render("▎")
	dim := l.styles.Dim
	width := max(l.width-6, 20)
	var lines []string
	if head != "" {
		lines = append(lines, rail+" "+head)
	}
	for i, label := range labels {
		for j, wl := range wrapHanging(pad(i+1)+label, width) {
			switch {
			case j > 0:
				lines = append(lines, rail+"      "+dim.Render(wl))
			case i == l.focus:
				lines = append(lines, rail+" "+l.styles.ButtonHot.Render("›"+wl))
			default:
				lines = append(lines, rail+"   "+wl)
			}
		}
	}
	lines = append(lines, rail+" "+dim.Render(hint))
	return strings.Join(lines, "\n")
}
