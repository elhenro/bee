package tui

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// caps in-flight rows so a big parallel batch can't push the input off
// screen; surplus folds into `+N more`
const maxPendingRows = 8

// in-flight calls: the `● Verb  args` a finished card opens with, bullet
// pulsing honey. frame drives the pulse between deltas
func (r *StreamRenderer) RenderPendingTools(uses []types.ToolUse, frame int) string {
	if len(uses) == 0 {
		return ""
	}
	r.pendingFrame = frame
	lines := make([]string, 0, len(uses))
	for _, u := range uses {
		// escalate hands control back, it isn't running
		if u.Name == "escalate" {
			continue
		}
		head, _ := r.callParts(u, outcomePending)
		lines = append(lines, clipLine(head, r.width-2))
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxPendingRows {
		hidden := len(lines) - (maxPendingRows - 1)
		lines = lines[:maxPendingRows-1]
		lines = append(lines, r.styles.Dim.Render(fmt.Sprintf("%s +%d more running", glyphMore, hidden)))
	}
	return r.gutter(strings.Join(lines, "\n"))
}
