package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// RenderStreaming returns the partial-text view while the model emits deltas.
// frame drives both the pre-token loader animation (empty partial) and the
// trailing caret animation (non-empty partial) — see animatedCaret for the
// rationale: a static caret looked "stopped" during in-stream pauses
// (reasoning, slow tool-call generation, network stalls between deltas).
//
// The partial is rendered as RAW text (not glamour-markdown). Re-rendering a
// growing buffer through glamour on every delta reflows word-wrap and shifts
// indent as markdown tokens (`-`, `*`, ```` ``` ````) come into being mid-
// stream — visually the text "jumps" and indentation breaks. Markdown styling
// is applied once the turn finishes in RenderMessage. Continuation lines are
// indented 2 cols so they align under the body column, not the role glyph.
func (r *StreamRenderer) RenderStreaming(partial string, frame int) string {
	// short-circuit when nothing changed since the last call. View() fires
	// on every key/tick/window event; without this the full markdown +
	// gutter pass runs ~120Hz on a 50KB partial during streaming.
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
		// no right caret while loading — keeps the row visually minimal.
		// blank line above so loader breathes; user prompt isn't squashed
		// against animation. Loader animation alone signals "bee working" —
		// the prefix ⬢ was redundant with the animated braille payload.
		var head string
		if r.showLoader {
			head = r.renderLoader(frame)
		} else {
			head = r.styles.RoleBee.Render("⬢")
		}
		if r.compact {
			return "\n" + head
		}
		return "\n" + outerGutter + head
	}
	// trim trailing whitespace so the caret sits flush with the last visible
	// char instead of floating on an indented blank line under the prose.
	trimmed := strings.TrimRight(partial, " \t\n")
	var body string
	if r.showLoader {
		body = trimmed + " " + r.animatedCaret(frame)
	} else {
		body = trimmed
	}
	if r.compact {
		return body
	}
	return applyGutter(body)
}

// RenderStreamingChunk formats a settled head of a streaming partial for
// emission to terminal scrollback via tea.Println. Mirrors RenderStreaming's
// gutter treatment but drops the trailing caret (the cursor isn't here
// anymore) and the leading blank row (rest of the partial keeps streaming
// in the live region right below). Used by the progressive-flush path so
// the head of a long response stays readable while the tail keeps growing.
//
// styleMarkdown=true routes the chunk through glamour so prose paragraphs
// keep their headings, lists, bold, and link styling once they hit scroll-
// back. Callers pass false when the chunk straddles an open ``` fence —
// partial code blocks render unpredictably through glamour, and raw output
// preserves any inline ANSI the model already painted (git log colors,
// chroma highlight, etc).
func (r *StreamRenderer) RenderStreamingChunk(chunk string, styleMarkdown bool) string {
	chunk = strings.TrimRight(chunk, "\n")
	if chunk == "" {
		return ""
	}
	body := chunk
	if styleMarkdown {
		body = r.renderText(chunk)
	}
	if r.compact {
		return body
	}
	return applyGutter(body)
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
	keep := maxRows - 1
	if keep < 1 {
		keep = 1
	}
	hidden := len(lines) - keep
	kept := lines[len(lines)-keep:]
	header := r.styles.Dim.Render(fmt.Sprintf("… +%d lines above", hidden))
	if !r.compact {
		header = outerGutter + header
	}
	return header + "\n" + strings.Join(kept, "\n")
}
