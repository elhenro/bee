package tui

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// RenderPendingTools draws the in-flight tool calls of the current turn: one
// row per call, `name  args  <swarm>`, the swarm being the same animated
// caret used at the tail of a streaming partial. Shown in the live region
// while the engine dispatches the batch, so N concurrent calls read as N
// buzzing bees instead of one generic loader. Cleared once the tool result
// message lands and the paired call+output cards flush to scrollback.
//
// frame drives the swarm so the bees drift even between deltas (the loader
// tick keeps firing for the whole turn). Reuses summarizeToolArgs +
// animatedCaret so a pending row matches its finalized card glyph-for-glyph.
func (r *StreamRenderer) RenderPendingTools(uses []types.ToolUse, frame int) string {
	if len(uses) == 0 {
		return ""
	}
	swarm := r.animatedCaret(frame)
	lines := make([]string, 0, len(uses))
	for _, u := range uses {
		// escalate isn't a running tool — it hands control back; skip it so
		// the live region doesn't show a buzzing "escalate" row.
		if u.Name == "escalate" {
			continue
		}
		name := r.styles.ToolName.Render(u.Name)
		line := name
		if args := summarizeToolArgs(u.Name, u.Input, r.argsBudget(u.Name)); args != "" {
			line += "  " + r.styles.ToolArgs.Render(args)
		}
		lines = append(lines, line+" "+swarm)
	}
	if len(lines) == 0 {
		return ""
	}
	// cap rows so a large parallel batch can't shove the input bar off the
	// bottom (bubbletea inline can't draw above the cursor). Surplus folds
	// into a single `+N more` line that keeps animating via the same swarm.
	const maxRows = 8
	if len(lines) > maxRows {
		hidden := len(lines) - (maxRows - 1)
		lines = lines[:maxRows-1]
		lines = append(lines, r.styles.Dim.Render(fmt.Sprintf("… +%d more running ", hidden))+swarm)
	}
	body := strings.Join(lines, "\n")
	if r.compact {
		return body
	}
	return applyGutter(body)
}
