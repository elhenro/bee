package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/types"
)

// live in-progress slice: streaming turn, compaction, or the latest error.
// past messages live in scrollback; empty when idle. maxRows clips the text
// to its tail: inline bubbletea can't draw above the cursor, so an
// unclipped long partial would push its newest tokens off screen
func (m Model) renderLive(maxRows int) string {
	var parts []string
	if m.state == StateStreaming {
		parts = append(parts, m.renderStreamingParts(maxRows)...)
	}
	// tutorial typewriter renders through the real stream renderer
	if m.tutorial.active && m.tutorial.typing {
		parts = append(parts, m.stream.RenderStreaming(m.tutorial.typed, m.loaderFrame))
	}
	if m.compacting || m.handoffing {
		parts = append(parts, m.renderCompactingRow())
	}
	// a submit that landed mid-compact, held until onCompactDone drains it
	if m.compacting && m.queuedMidCompact != "" {
		parts = append(parts, m.renderQueuedMidCompact())
	}
	if m.state == StateError && m.lastErr != "" {
		parts = append(parts, m.renderErrorBlock(m.lastErr))
	}
	if m.escalate.Active {
		parts = append(parts, m.escalate.View())
	}
	if m.planmode.Active {
		parts = append(parts, m.planmode.View())
	}
	return strings.Join(parts, "\n")
}

// live turn figures for the busy row and legacy loaders: input tokens from
// the cost tracker's last event, output from the char count (÷4 estimate)
func (m Model) loaderStatsNow() LoaderStats {
	in := 0
	outChars := m.turnOutChars
	outTok := int(float64(outChars) / charsPerToken)
	if m.costs != nil {
		in = m.costs.LastInput()
		// queen/hive runs on sub-engines, so no deltas bump turnOutChars;
		// fall back to the tracker's output so the figure isn't stuck at 0
		d := m.costs.Total().Output - m.turnStartOutput
		outTok, outChars = max(outTok, d), max(outChars, d)
	}
	return LoaderStats{
		InTokens:  in,
		OutChars:  outChars,
		OutTokens: outTok,
		// smoothed: no 1-tick flash on first batch or reasoning→answer
		Rate:     int(m.loaderRateEMA + 0.5),
		RateTokS: m.loaderRateTokS,
		Seed:     m.loaderSeed,
		// consecutive idle ticks: the busy-row dance reads it as form
		StallFrames: m.loaderStallFrames,
		ShowIn:      m.showLoaderIn,
		ShowOut:     m.showLoaderOut,
		ShowRate:    m.showLoaderRate,
	}
}

// reasoning, answer text, running calls, busy row; pinned braille loaders
// keep their own pre-token/caret behavior
func (m Model) renderStreamingParts(maxRows int) []string {
	m.stream.SetLoaderStats(m.loaderStatsNow())
	// a progressive flush may already have pushed a prefix to scrollback:
	// render only what's live, under the marker that prefix printed
	visible := m.partial
	if n := len(m.streamFlushed); n > 0 && n <= len(m.partial) {
		visible = m.partial[n:]
	}
	m.stream.SetContinuation(len(m.streamFlushed) > 0)

	var parts []string
	// reasoning streams above the answer, styled like the committed block;
	// hidden thoughts still show their label row with the running clock
	thinkRows := 0
	if think := m.thinkPartial; think != "" {
		elapsed, running := m.thinkElapsed()
		block := m.stream.RenderThinkingPartial(think, elapsed, running)
		if maxRows > 0 && m.showThoughts {
			// share the budget with the answer text; reasoning alone may use it all
			room := maxRows - m.busyRows()
			if visible != "" {
				room /= 2
			} else {
				room--
			}
			if room >= 3 {
				block = m.stream.ClipStreamingTail(block, room)
			}
		}
		if !m.showThoughts {
			thinkRows = lipgloss.Height(block)
		}
		parts = append(parts, block)
	}
	if !m.stream.isDefaultLoader() {
		return append(parts, m.renderLegacyLoader(visible, maxRows))
	}

	pending := ""
	if len(m.pendingTools) > 0 {
		pending = m.stream.RenderPendingTools(m.pendingTools, m.loaderFrame)
	}
	if text := m.stream.RenderStreaming(visible, m.loaderFrame); text != "" {
		if clip := maxRows - m.busyRows() - thinkRows; maxRows > 0 && clip > 0 {
			text = m.stream.ClipStreamingTail(text, clip)
		}
		parts = append(parts, text)
	}
	if pending != "" {
		if len(parts) == 0 && !m.compact && !m.stream.afterTool {
			pending = "\n" + pending
		}
		parts = append(parts, pending)
	}
	return append(parts, m.renderBusyRow())
}

// rows the busy row and running calls claim under the streaming text; the
// text clip and the flush budget both subtract it
func (m Model) busyRows() int {
	if m.stream == nil || !m.stream.isDefaultLoader() {
		return 0
	}
	n := 1
	if !m.compact {
		n++ // blank row above the spinner
	}
	if len(m.pendingTools) > 0 {
		n += lipgloss.Height(m.stream.RenderPendingTools(m.pendingTools, m.loaderFrame))
	}
	return n
}

// the spinner line, with a blank row above outside compact mode
func (m Model) renderBusyRow() string {
	row := m.stream.RenderSpinner(m.loaderFrame, m.turnElapsed())
	// the model is blocked on its calls: say so, and clock the wait, not the turn
	if label := waitingLabel(m.pendingTools); label != "" {
		row = m.stream.RenderSpinnerLabel(m.loaderFrame, label, time.Since(m.pendingSince), interruptHint)
	}
	if m.compact {
		return row
	}
	return "\n" + row
}

// pinned braille loaders (BEE_LOADER=<name>): pre-token animation, then
// text with the animated caret and, for strip, the readout row below
func (m Model) renderLegacyLoader(visible string, maxRows int) string {
	if len(m.pendingTools) > 0 && visible == "" {
		return m.stream.RenderPendingTools(m.pendingTools, m.loaderFrame)
	}
	// the strip takes a row below the text; reserve it from the clip
	var strip string
	if visible != "" {
		strip = m.stream.RenderTokenStrip(m.loaderFrame)
	}
	clip := maxRows
	if strip != "" && clip > 1 {
		clip--
	}
	out := m.stream.RenderStreaming(visible, m.loaderFrame)
	if clip > 0 && visible != "" {
		out = m.stream.ClipStreamingTail(out, clip)
	}
	if strip != "" {
		out += "\n" + strip
	}
	return out
}

// busy row for /compact and /handoff
func (m Model) renderCompactingRow() string {
	if !m.stream.isDefaultLoader() {
		return m.stream.RenderCompacting(m.loaderFrame)
	}
	label := "Compacting context…"
	if m.handoffing {
		label = "Writing handoff…"
	}
	row := m.stream.RenderSpinnerLabel(m.loaderFrame, label, 0, "")
	if m.compact {
		return row
	}
	return "\n" + row
}

// busy-row label while calls run: the tool's name for one, a count for a batch
func waitingLabel(uses []types.ToolUse) string {
	var names []string
	for _, u := range uses {
		if u.Name != "escalate" {
			names = append(names, u.Name)
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "Running " + names[0] + "…"
	}
	return fmt.Sprintf("Running %d calls…", len(names))
}
