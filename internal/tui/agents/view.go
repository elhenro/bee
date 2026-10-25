package agents

import (
	"fmt"
	"strings"
)

func (m model) View() string {
	if m.settingsPane.isOpen() {
		return m.settingsPane.view(m.width, m.height)
	}
	var b strings.Builder
	b.WriteString(renderHeader(m.all, m.pendingModel, m.pendingProvider, m.prefs))
	b.WriteString("\n\n")

	if len(m.flat) == 0 {
		b.WriteString(dimStyle.Render("  no agents yet — type a task below and press enter to spawn one."))
		b.WriteString("\n\n")
	} else {
		idx := 0
		for _, sec := range m.sections {
			if sec.kind == secMerged && !m.prefs.ShowMerged {
				// hidden section still consumes the flat-index range so the
				// arrow-key cursor stays in sync with what's drawn.
				idx += len(sec.rows)
				continue
			}
			style := sectionStyle
			if sec.kind == secErrors {
				style = errSectionStyle
			}
			label := " " + style.Render(sec.kind.title()) + dimStyle.Render(fmt.Sprintf(" · %d", len(sec.rows)))
			label += dimStyle.Render(sectionHint(sec))
			b.WriteString(label)
			b.WriteString("\n")
			for _, r := range sec.rows {
				selected := idx == m.sel
				b.WriteString(renderRow(r, selected, m.width, m.prefs))
				b.WriteString("\n")
				idx++
			}
			b.WriteString("\n")
		}
	}

	if m.palette.Active {
		b.WriteString(m.palette.View())
		b.WriteString("\n")
	}
	if m.picker != nil && m.picker.Active() {
		b.WriteString(m.picker.View())
		b.WriteString("\n")
	}
	b.WriteString(m.input.View())
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(badStyle.Render("• " + m.notice))
	}
	return b.String()
}
