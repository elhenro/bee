package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// caps in-flight rows so a big parallel batch can't push the input off
// screen; surplus folds into `+N more`
const maxPendingRows = 8

// SetLiveTail names the running call and its latest output; the next
// RenderPendingTools hangs those rows under it.
func (r *StreamRenderer) SetLiveTail(useID, text string) {
	r.tailID, r.tailText = useID, text
}

// in-flight calls: the `● Verb  args` a finished card opens with, bullet
// pulsing honey. frame drives the pulse between deltas
func (r *StreamRenderer) RenderPendingTools(uses []types.ToolUse, frame int) string {
	r.pendingFrame = frame
	var heads, ids []string
	for _, u := range uses {
		// escalate hands control back, it isn't running
		if u.Name == "escalate" {
			continue
		}
		head, _ := r.callParts(u, outcomePending, 0)
		heads = append(heads, clipLine(head, r.width-2))
		ids = append(ids, u.ID)
	}
	if len(heads) == 0 {
		return ""
	}
	hidden := 0
	if len(heads) > maxPendingRows {
		hidden = len(heads) - (maxPendingRows - 1)
		heads, ids = heads[:maxPendingRows-1], ids[:maxPendingRows-1]
	}
	lines := make([]string, 0, len(heads)+tools.LiveTailLines+1)
	for i, head := range heads {
		lines = append(lines, head)
		if ids[i] == r.tailID {
			lines = append(lines, r.tailRows()...)
		}
	}
	if hidden > 0 {
		lines = append(lines, r.styles.Dim.Render(fmt.Sprintf("%s +%d more running", glyphMore, hidden)))
	}
	return r.gutter(strings.Join(lines, "\n"))
}

// the running call's last output rows, hung off the connector like a finished
// result; raw process bytes, so ANSI-stripped and tab-expanded
func (r *StreamRenderer) tailRows() []string {
	if r.tailText == "" {
		return nil
	}
	w := r.textWidth(resultIndent)
	var rows []string
	for ln := range strings.SplitSeq(expandTabs(ansi.Strip(r.tailText)), "\n") {
		rows = append(rows, clipLine(r.styles.ToolPrev.Render(ln), w))
	}
	if len(rows) > tools.LiveTailLines {
		rows = rows[len(rows)-tools.LiveTailLines:]
	}
	return strings.Split(hang(strings.Join(rows, "\n"), r.connector(), strings.Repeat(" ", resultIndent)), "\n")
}
