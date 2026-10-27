package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/types"
)

// rows of full call a card shows before folding the rest; verbose lifts it
const callDetailRows = 12

// runes of the call summarized into the detail row: effectively all of it
const callDetailBudget = 1 << 16

// a call the model labeled: the label stands in for the raw args on the head
// row, and the whole call hangs below when the setting is on, verbose is on,
// or the call failed (the command that broke is the story)
func (r *StreamRenderer) labeledCall(prefix string, u types.ToolUse, o toolOutcome) (head, detail string) {
	head = prefix + "  " + r.styles.ToolArgs.Render(truncateRunes(cleanCallText(u.Label(), false), r.argsBudget(u.Name)))
	if r.showCallDetail || r.verbose || o == outcomeFail || o == outcomeRefused {
		detail = r.callDetail(u)
	}
	return head, detail
}

// the whole call in the secondary tone, wrapped, under a dotted connector
func (r *StreamRenderer) callDetail(u types.ToolUse) string {
	full := summarizeToolArgs(u.Name, u.Args(), callDetailBudget)
	if strings.TrimSpace(full) == "" {
		return ""
	}
	w := r.textWidth(resultIndent)
	var rows []string
	for ln := range strings.SplitSeq(cleanCallText(expandTabs(full), true), "\n") {
		rows = append(rows, strings.Split(wrapText(ln, w), "\n")...)
	}
	if hidden := len(rows) - callDetailRows; !r.verbose && hidden > 0 {
		rows = rows[:callDetailRows]
		rows = append(rows, fmt.Sprintf("%s +%d lines", glyphMore, hidden))
	}
	for i, ln := range rows {
		rows[i] = r.styles.ToolPrev.Render(ln)
	}
	lead := "  " + r.styles.ToolRail.Render(glyphDetail) + "  "
	return hang(strings.Join(rows, "\n"), lead, lead)
}

// model-written call text stripped of escape sequences and every control rune
// (C0, DEL, C1) so it cannot drive the terminal; newlines survive only when
// keepNL, for the multi-line detail row
func cleanCallText(s string, keepNL bool) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' && keepNL {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
