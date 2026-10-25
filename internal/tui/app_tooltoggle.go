package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elhenro/bee/internal/types"
)

// toggleLastToolOutput re-prints the most recent tool result at the flipped
// detail level. Scrollback lines are immutable once flushed via tea.Println,
// so a toggle can't edit the original card in place — it appends a fresh copy
// (full ↔ collapsed) below. State flips per press.
func (m Model) toggleLastToolOutput() (tea.Model, tea.Cmd) {
	msg, ok := lastToolResultMessage(m.messages)
	if !ok {
		return m, nil
	}
	m.toolOutputExpanded = !m.toolOutputExpanded
	m.lastToolClipped = false
	rendered := m.stream.RenderMessageDetail(msg, m.toolOutputExpanded)
	if rendered == "" {
		return m, nil
	}
	return m, m.scroll.print(m.hardwrap(rendered))
}

// lastToolResultMessage walks messages backward for the latest one carrying a
// tool-result block. Edit/write/escalate results render empty, but those are
// rare as the final tool of a turn; callers tolerate an empty render.
func lastToolResultMessage(msgs []types.Message) (types.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		for _, b := range msgs[i].Content {
			if b.Type == types.BlockToolResult && b.Result != nil {
				return msgs[i], true
			}
		}
	}
	return types.Message{}, false
}
