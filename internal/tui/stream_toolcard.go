package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/tools/read"
	"github.com/elhenro/bee/internal/tools/why"
	"github.com/elhenro/bee/internal/types"
)

// colors a call's bullet: pending while it runs, then green/red/amber
type toolOutcome int

const (
	outcomeDone toolOutcome = iota
	outcomeFail
	outcomeRefused
	outcomePending
)

// preview budgets in result rows; reads collapse to a count (the model
// needed the content, you rarely do), verbose lifts every cap
const (
	previewBash    = 5
	previewDefault = 3
	previewError   = 6
)

func (r *StreamRenderer) bullet(o toolOutcome) string {
	switch o {
	case outcomeFail:
		return r.styles.ToolFail.Render(glyphTool)
	case outcomeRefused:
		return r.styles.Warn.Render(glyphTool)
	case outcomePending:
		return r.pendingBullet()
	default:
		return r.styles.ToolOK.Render(glyphTool)
	}
}

// pendingBullet is the in-flight marker: the call bullet breathing between
// honey and faint. Resolves into the static outcome glyph when the call
// finishes.
func (r *StreamRenderer) pendingBullet() string {
	if !r.showLoader {
		return r.styles.Mark.Render(glyphTool)
	}
	return r.styles.Mark.Foreground(pulseColor(r.pendingFrame)).Render(glyphTool)
}

// lead of a result's first row
func (r *StreamRenderer) connector() string {
	return "  " + r.styles.ToolRail.Render(glyphResult) + "  "
}

// header row plus, for file mutations, the diff body; a failed edit drops
// the diff, the error is the story, not the change that didn't land. anchor
// numbers an edit's rows (0 = unknown)
func (r *StreamRenderer) callParts(u types.ToolUse, o toolOutcome, anchor int) (head, body string) {
	// the args budget has a floor so a narrow terminal still shows a few
	// words, which can overshoot the row: clip, a wrapped head tears the left edge
	defer func() { head = clipLine(head, r.textWidth(0)) }()
	prefix := r.bullet(o) + " " + r.styles.ToolName.Render(toolTitle(u.Name))
	if h, b, ok := r.renderEditPreview(u, anchor); ok {
		if h != "" {
			prefix += "  " + h
		}
		if o == outcomeFail || o == outcomeRefused {
			b = ""
		}
		if w := r.whyRow(u); w != "" {
			if b != "" {
				w += "\n"
			}
			b = w + b
		}
		return prefix, b
	}
	if u.Label() != "" {
		return r.labeledCall(prefix, u, o)
	}
	args := summarizeToolArgs(u.Name, u.Input, r.argsBudget(u.Name))
	switch {
	case args == "":
		return prefix, ""
	case u.Name == "bash" && r.highlight:
		return prefix + "  " + r.hlLang(args, "bash"), ""
	}
	return prefix + "  " + r.styles.ToolArgs.Render(args), ""
}

// the model's one-line reason for a mutation, hung under the call header;
// "" when the call carried none
func (r *StreamRenderer) whyRow(u types.ToolUse) string {
	text := why.Of(u.Input)
	if text == "" {
		return ""
	}
	rows := strings.Split(wrapText(r.styles.ToolPrev.Render(expandTabs(ansi.Strip(text))), r.textWidth(resultIndent)), "\n")
	if !r.verbose && len(rows) > 2 {
		rows = append(rows[:1], r.styles.Dim.Render(glyphMore))
	}
	return hang(strings.Join(rows, "\n"), r.connector(), strings.Repeat(" ", resultIndent))
}

// bare call card; used for escalate and calls without a result
func (r *StreamRenderer) renderToolUse(u types.ToolUse) string {
	if u.Name == "escalate" {
		return r.renderEscalate(u)
	}
	head, body := r.callParts(u, outcomeDone, 0)
	if body == "" {
		return head + "\n"
	}
	return head + "\n" + body + "\n"
}

// call above its result so batched calls read call → output → call →
// output; the use comes from r.toolUses (indexed by RenderMessage), orphan
// results render bare
func (r *StreamRenderer) pairedToolResult(res types.ToolResult) string {
	use, ok := r.toolUses[res.UseID]
	if !ok {
		return r.renderToolResult(res)
	}
	// escalate renders its full card from the use side already.
	if use.Name == "escalate" {
		return ""
	}
	v := r.viewResult(use, res)
	head, diff := r.callParts(use, outcomeFor(res, v), editAnchor(use, res))
	out := head + "\n"
	if diff != "" {
		out += diff + "\n"
	}
	if body := r.resultBody(use, res, v); body != "" {
		out += body + "\n"
	}
	return out
}

// just the result rows
func (r *StreamRenderer) renderToolResult(res types.ToolResult) string {
	use := r.toolUses[res.UseID]
	body := r.resultBody(use, res, r.viewResult(use, res))
	if body == "" {
		return ""
	}
	return body + "\n"
}

// result cleaned for display
type resultView struct {
	lines   []string
	total   int  // bytes of cleaned payload, for the "+N lines · size" hint
	refused bool // blocked by safety/approval rather than failed
}

// strips ansi/control bytes and guard nudges, drops blank rows, flags
// refusals ("refused…" from shell/safety, "denied by" from write filters):
// blocked, not broken
func (r *StreamRenderer) viewResult(use types.ToolUse, res types.ToolResult) resultView {
	content := res.Content
	if use.Name == "read" && !res.IsError {
		// the soft "unchanged since your earlier read" note is a model-only
		// nudge appended to the real content; hide it from the human view.
		content = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(content, "\n"),
			"(note: unchanged since your earlier read this session)"), "\n")
	}
	clean := expandTabs(ansi.Strip(content))
	if !r.showNudges {
		clean = stripGuardPrefixes(clean)
	}
	lines := compactLines(clean)
	if use.Name != "read" && !res.IsError {
		lines = foldArt(clean, r.verbose)
	}
	refused := res.IsError && len(lines) > 0 &&
		(strings.HasPrefix(lines[0], "refused") || strings.Contains(lines[0], "denied by"))
	return resultView{lines: lines, total: len(clean), refused: refused}
}

func outcomeFor(res types.ToolResult, v resultView) toolOutcome {
	switch {
	case v.refused:
		return outcomeRefused
	case res.IsError:
		return outcomeFail
	}
	return outcomeDone
}

// result rows a tool earns, -1 = no cap
func (r *StreamRenderer) previewRows(name string, isErr bool) int {
	switch {
	case r.verbose:
		return -1
	case isErr:
		return previewError
	case name == "read":
		return 0
	case name == "bash":
		return previewBash
	}
	return previewDefault
}

// rows hanging off a call; edit successes return "" (the diff says it all,
// the text is model-only bookkeeping)
func (r *StreamRenderer) resultBody(use types.ToolUse, res types.ToolResult, v resultView) string {
	if len(v.lines) == 0 || !res.IsError && isEditTool(use.Name) {
		return ""
	}
	style := r.styles.ToolPrev
	switch {
	case v.refused:
		style = r.styles.Warn
	case res.IsError:
		style = r.styles.Error
	}
	n := r.previewRows(use.Name, res.IsError)
	if n == 0 && use.Name == "read" && len(v.lines) == 1 && strings.HasPrefix(v.lines[0], read.NotePrefix) {
		n = 1 // an attached picture names itself, a line count would not
	}
	var rows []string
	if n == 0 {
		rows = []string{style.Render(readSummary(len(v.lines), v.total))}
	} else {
		shown := v.lines
		if n > 0 {
			shown = shown[:min(n, len(shown))]
		}
		styleRow, w := r.rowStyler(use, res, style), r.textWidth(resultIndent)
		for _, ln := range shown {
			row := styleRow(shortenPathsInline(ln))
			if r.verbose {
				rows = append(rows, strings.Split(wrapText(row, w), "\n")...)
			} else {
				rows = append(rows, clipLine(row, w))
			}
		}
		if hidden := len(v.lines) - len(shown); hidden > 0 {
			r.clipped = true
			rows = append(rows, r.styles.Dim.Render(withSize(fmt.Sprintf("%s +%d lines", glyphMore, hidden), v.total)))
		}
	}
	return hang(strings.Join(rows, "\n"), r.connector(), strings.Repeat(" ", resultIndent))
}
