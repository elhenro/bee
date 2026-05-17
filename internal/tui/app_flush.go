package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/types"
)

// flush emits tea.Println for every message in m.messages[printedCount:]
// and advances printedCount. The returned Cmd is nil when there's nothing
// new. Inline-mode bubbletea prints those lines above the live region so
// they fall into the terminal's native scrollback — selectable, copyable,
// and unaffected by our redraws.
//
// printedCount can exceed len(m.messages) after a session swap or fork
// that resets the slice; in that case we re-anchor it without emitting.
//
// pendingFlushedPrefix (set by commitFlushed when a partial-flushed stream
// finalized) gets consumed on the matching assistant message: its first
// text block has the prefix stripped so the progressively-flushed head
// doesn't print twice. Always cleared after this call, even on no match.
func (m *Model) flush() tea.Cmd {
	if m.printedCount > len(m.messages) {
		m.printedCount = len(m.messages)
	}
	if m.printedCount == len(m.messages) {
		m.pendingFlushedPrefix = ""
		return nil
	}
	pending := m.messages[m.printedCount:]
	startIdx := m.printedCount
	m.printedCount = len(m.messages)
	prefix := m.pendingFlushedPrefix
	m.pendingFlushedPrefix = ""
	cmds := make([]tea.Cmd, 0, len(pending))
	for i, msg := range pending {
		// strip the already-flushed head off the first matching assistant
		// turn so its prefix doesn't render twice in scrollback.
		if prefix != "" && msg.Role == types.RoleAssistant {
			if stripped, ok := stripTextPrefix(msg, prefix); ok {
				msg = stripped
			}
			prefix = ""
		}
		rendered := m.stream.RenderMessage(msg)
		// renderer may return empty for filtered messages (e.g. hidden
		// [nudge] turns); skip those so we don't blit a stray blank row.
		if rendered == "" {
			continue
		}
		// blank-line gap between turns so scrollback breathes. Skip before
		// the very first message of the session (startIdx+i == 0) so we
		// don't push a stray gap above the chat history on cold start.
		// Compact mode drops the gap entirely — denser layout for users who
		// opted in via ctrl+v / BEE_COMPACT.
		if startIdx+i > 0 && !m.compact {
			rendered = "\n" + rendered
		}
		cmds = append(cmds, tea.Println(rendered))
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Sequence(cmds...)
}

// commitFlushed transfers any progressively-flushed prefix from the active
// stream over to pendingFlushedPrefix so the next flush() call can suppress
// the already-printed head from the final assistant message. Must be called
// before m.partial is cleared on any successful or canceled stream path.
func (m *Model) commitFlushed() {
	if m.streamFlushed == "" {
		m.streamFenceOpen = false
		return
	}
	m.pendingFlushedPrefix = m.streamFlushed
	m.streamFlushed = ""
	m.streamFenceOpen = false
}

// maybeFlushPartialHead pushes complete leading lines of m.partial into
// terminal scrollback when the partial would otherwise overflow the live
// region. bubbletea's inline renderer cannot reach above the cursor, so a
// long response normally has its head clipped out of sight while only the
// tail (with a `… +N above` header) stays visible. Progressive flush emits
// settled head lines via tea.Println so the user can scroll up and read
// from the start while the tail keeps streaming.
//
// Returns nil when nothing to flush, progressive mode is off, no complete
// line is available, or the partial still fits the live budget. Callers
// should invoke this after appending to m.partial; the newline gate keeps
// the per-delta cost negligible.
func (m *Model) maybeFlushPartialHead() tea.Cmd {
	if !m.progressiveStream || m.partial == "" || m.height <= 0 || m.width <= 0 {
		return nil
	}
	flushedLen := len(m.streamFlushed)
	if flushedLen > len(m.partial) {
		// defensive: partial got reset without commitFlushed (shouldn't
		// happen — every reset path commits — but bail rather than slice
		// past length).
		m.streamFlushed = ""
		m.streamFenceOpen = false
		return nil
	}
	unflushed := m.partial[flushedLen:]
	// flush at paragraph boundary (\n\n), not per-line. Glamour styles prose
	// holistically (headings, paragraph wrap, inline code) — chunking by line
	// strips it of context and most lines fail `needsMarkdown`, so prose
	// flushes as raw text. Paragraph-grained flushes restore the formatting
	// while still relieving the live region for long responses.
	lastNL := strings.LastIndex(unflushed, "\n\n")
	if lastNL < 0 {
		return nil
	}
	lastNL++ // include the first \n of the pair; trailing \n stays in unflushed
	// only flush when the live region actually overflows. Short responses
	// stay in the live buffer and get full markdown styling at finalization.
	intro := m.renderIntro()
	bot := m.renderBottomBar()
	status := m.renderTopBar()
	warn := m.renderWarning()
	var ctxBar string
	if m.showContextBar {
		ctxBar = m.renderContextBar()
	}
	budget := liveBudget(m.height, intro, bot, status, warn, ctxBar)
	if budget <= 0 {
		return nil
	}
	rendered := m.stream.RenderStreaming(unflushed, m.loaderFrame)
	w := m.width
	if w < 4 {
		w = 80
	}
	wrapped := ansi.Hardwrap(rendered, w, true)
	if strings.Count(wrapped, "\n")+1 <= budget {
		return nil
	}
	head := unflushed[:lastNL+1]
	// glamour-style only when the chunk lives entirely outside an open
	// ``` fence: partial code blocks render unpredictably through glamour
	// (mojibake, lost monospace), while raw output keeps inline ANSI the
	// model may have already painted (git log, chroma, etc).
	startOpen := m.streamFenceOpen
	endOpen := fenceTransitionsAfter(head, startOpen)
	styleMarkdown := !startOpen && !endOpen
	chunk := m.stream.RenderStreamingChunk(head, styleMarkdown)
	m.streamFlushed += head
	m.streamFenceOpen = endOpen
	if chunk == "" {
		return nil
	}
	return tea.Println(chunk)
}

// stripTextPrefix returns a copy of msg with the leading prefix removed
// from its first BlockText. ok is false when no text block starts with the
// prefix — in that case the caller should fall back to printing msg as-is.
func stripTextPrefix(msg types.Message, prefix string) (types.Message, bool) {
	for i, b := range msg.Content {
		if b.Type != types.BlockText {
			continue
		}
		if !strings.HasPrefix(b.Text, prefix) {
			return msg, false
		}
		out := msg
		out.Content = make([]types.ContentBlock, len(msg.Content))
		copy(out.Content, msg.Content)
		out.Content[i].Text = b.Text[len(prefix):]
		return out, true
	}
	return msg, false
}
