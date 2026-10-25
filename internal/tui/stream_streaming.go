package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// partial-text view while the model streams. with the default loader it's
// text only (the busy row is separate), so it caches across ticks; pinned
// braille loaders also draw their pre-token animation and caret here.
//
// the tail paragraph is raw: re-rendering a growing buffer through glamour
// reflows wrap and indent as markdown tokens appear mid-stream. settled
// paragraphs are styled (streamMarkdown).
func (r *StreamRenderer) RenderStreaming(partial string, frame int) string {
	if r.isDefaultLoader() {
		frame = 0
	}
	// View fires on every key/tick/window event; skip the wrap pass when
	// nothing changed
	if r.cachedStreamValid && r.cachedStreamPartial == partial && r.cachedStreamFrame == frame {
		return r.cachedStreamOutput
	}
	out := r.renderStreamingUncached(partial, frame)
	r.cachedStreamPartial = partial
	r.cachedStreamFrame = frame
	r.cachedStreamOutput = out
	r.cachedStreamValid = true
	return out
}

func (r *StreamRenderer) renderStreamingUncached(partial string, frame int) string {
	if partial == "" {
		if r.isDefaultLoader() {
			return "" // the busy row carries the wait
		}
		// pinned painter: the art alone says "working"
		head := r.styles.Mark.Render(glyphBee)
		if r.showLoader {
			head = r.renderLoader(frame)
		}
		return "\n" + r.gutter(head)
	}
	// trim trailing whitespace so the caret sits flush with the last visible
	// char instead of floating on an indented blank line under the prose.
	// tabs skip cells without erasing them: redrawn rows would keep stale
	// characters under a tab
	body := expandTabs(strings.TrimRight(partial, " \t\n"))
	if body == "" {
		return ""
	}
	if r.isDefaultLoader() {
		return r.proseBlock(r.streamMarkdown(body))
	}
	if r.showLoader {
		body += " " + r.animatedCaret(frame)
	}
	return r.proseBlock(wrapText(body, r.textWidth(markIndent)))
}

// finished paragraphs (up to the last blank line outside a fence) go
// through glamour as one document, the one being typed stays raw. the
// settled part re-renders only when a paragraph lands, not per delta
func (r *StreamRenderer) streamMarkdown(text string) string {
	settled, tail := splitSettled(text)
	var out string
	if settled != "" {
		if r.settledSrc != settled {
			r.settledSrc, r.settledOut = settled, r.renderText(settled)
		}
		out = r.settledOut
	}
	if tail = strings.TrimSpace(tail); tail != "" {
		if out != "" {
			out += "\n\n"
		}
		out += wrapText(tail, r.textWidth(markIndent))
	}
	return out
}

// cut after the last paragraph break outside a ``` fence; before = settled,
// after = still being written
func splitSettled(text string) (settled, tail string) {
	fence, cut, off := false, 0, 0
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "```") {
			fence = !fence
		}
		off += len(ln) + 1
		if !fence && i > 0 && strings.TrimSpace(ln) == "" && strings.TrimSpace(lines[i-1]) != "" {
			cut = off
		}
	}
	if cut <= 0 || cut > len(text) {
		return "", text
	}
	return strings.TrimRight(text[:cut], "\n"), text[cut:]
}

// marker, gutter and, outside compact, the blank row above
func (r *StreamRenderer) proseBlock(body string) string {
	body = r.gutter(r.renderAssistant(body))
	if r.compact {
		return body
	}
	return "\n" + body
}

// settled head of a streaming partial for tea.Println (progressive flush):
// same marker/gutter/blank row as the live view, no caret.
//
// styleMarkdown=true runs glamour so headings, lists and links survive
// into scrollback; callers pass false when the chunk straddles an open ``` fence:
// partial code blocks render unpredictably through glamour, and raw output
// preserves any inline ANSI the model already painted (git log colors,
// chroma highlight, etc).
func (r *StreamRenderer) RenderStreamingChunk(chunk string, styleMarkdown bool) string {
	chunk = strings.TrimRight(chunk, "\n")
	if chunk == "" {
		return ""
	}
	if styleMarkdown {
		return r.proseBlock(r.renderText(chunk))
	}
	return r.proseBlock(chunk)
}

// fenceTransitionsAfter returns the fence-open state that results from
// applying chunk's ``` toggles on top of startOpen. A line counts as a
// fence marker when its first non-whitespace token starts with ```.
func fenceTransitionsAfter(chunk string, startOpen bool) bool {
	open := startOpen
	for _, line := range strings.Split(chunk, "\n") {
		t := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(t, "```") {
			open = !open
		}
	}
	return open
}

// ClipStreamingTail keeps the last maxRows visual rows of a rendered
// streaming chunk, prepending a `… +N lines above` header when content was
// dropped. Visual rows are computed against r.width so soft-wrapped long
// lines count correctly; bubbletea's inline renderer cannot reach above the
// cursor, so without this the head of a long partial gets clipped out of
// sight while it grows. maxRows <= 0 is a no-op (caller has no budget info).
// With progressive flush active, complete leading lines get pushed to
// scrollback before this fires — clipping only trims the unflushed tail.
func (r *StreamRenderer) ClipStreamingTail(rendered string, maxRows int) string {
	if maxRows <= 0 || rendered == "" {
		return rendered
	}
	w := r.width
	if w < 4 {
		w = 80
	}
	wrapped := ansi.Hardwrap(rendered, w, true)
	lines := strings.Split(wrapped, "\n")
	if len(lines) <= maxRows {
		return rendered
	}
	// reserve 1 row for the `… +N above` header so the indicator never
	// pushes the tail off the bottom.
	keep := max(maxRows-1, 1)
	hidden := len(lines) - keep
	kept := lines[len(lines)-keep:]
	header := r.styles.Dim.Render(fmt.Sprintf("… +%d lines above", hidden))
	if !r.compact {
		header = outerGutter + header
	}
	return header + "\n" + strings.Join(kept, "\n")
}
