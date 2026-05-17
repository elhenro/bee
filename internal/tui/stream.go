package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/types"
)

// argsSummaryCompact / argsSummaryVerbose cap tool-input json so cards stay
// one-liners; verbose mode lets longer key paths through.
const argsSummaryCompact = 40
const argsSummaryVerbose = 120

// previewLinesCompact sets the tool-output preview height for compact mode.
// Raised from 1 → 5 so short shell output (typical 2-4 lines: build ok,
// test ok, ls of a small dir) renders in full instead of getting collapsed
// to `+N more`. Verbose mode bypasses the cap entirely — every non-blank
// line of the tool output renders.
const previewLinesCompact = 5

// OSC 133 prompt-zone marks. Terminals with shell integration (iTerm,
// Ghostty, wezterm) let cmd+↑/↓ jump between user/assistant turns in
// scrollback. Terminals without integration see them as silent escapes.
const (
	osc133Start = "\x1b]133;A\x07"
	osc133End   = "\x1b]133;B\x07\x1b]133;C\x07"
)

// outerGutter is the single-space left padding prepended to every rendered
// line. Universal paddingX=1 so content never touches col 0.
const outerGutter = " "

// applyGutter prefixes every non-empty line of s with outerGutter. Empty
// lines stay empty so collapseBlankRuns sees them as blank, not "space-only".
func applyGutter(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			continue
		}
		lines[i] = outerGutter + l
	}
	return strings.Join(lines, "\n")
}

// StreamRenderer turns Messages and live deltas into styled strings.
// It is an append-only view buffer — callers feed it events and read View().
type StreamRenderer struct {
	styles       Styles
	md           *glamour.TermRenderer
	width        int
	verbose      bool
	showThoughts bool
	// showNudges gates rendering of synthetic `[nudge]` user messages the
	// loop injects on reasoning-only stalls. Default false = hidden. The
	// loop still emits + persists these turns; the filter is render-only.
	showNudges  bool
	loaderStyle LoaderStyle
	// compact strips the spacing layer for terminals where vertical
	// density matters more than focus. Default false = clean mode.
	compact bool
	// highlight gates chroma syntax-highlighting on tool output, file
	// content, edit/write diffs, and bash command summaries. Default true.
	highlight bool
	// showLoader gates the braille "generating" animation (pre-token loader
	// + in-stream animated caret). Off renders a single static ⬢ row while
	// waiting and drops the caret while text streams. Default true.
	showLoader bool
	// toolUses indexes tool calls by ID so renderToolResult can recover the
	// originating cmd/args (e.g. surface the failed bash command in place of
	// the bare "exit N" preview). Populated lazily as RenderMessage walks
	// each turn.
	toolUses map[string]types.ToolUse
	// compactingVariant selects one of the three /compact swarm animations.
	// Picked at frame 0 of each compacting run, sticks for the duration.
	compactingVariant int
}

// SetLoaderStyle picks which pre-token loader animation to render.
func (r *StreamRenderer) SetLoaderStyle(s LoaderStyle) { r.loaderStyle = s }

// SetVerbose toggles full tool-output rendering. Compact (default) keeps
// the preview at one line; verbose lets the whole output through.
func (r *StreamRenderer) SetVerbose(v bool) { r.verbose = v }

// SetShowThoughts toggles BlockThinking chain-of-thought rendering. Off
// hides reasoning blocks entirely from scrollback; on (default) shows them
// dimmed and italicized.
func (r *StreamRenderer) SetShowThoughts(v bool) { r.showThoughts = v }

// SetShowNudges toggles rendering of synthetic `[nudge]` user messages.
// Off (default) collapses them out of scrollback; the loop still injects
// them so the provider sees the same conversation.
func (r *StreamRenderer) SetShowNudges(v bool) { r.showNudges = v }

// SetHighlight toggles chroma syntax-highlighting across diff/file/bash
// previews. Off returns raw content; on (default) emits ANSI-colored tokens.
func (r *StreamRenderer) SetHighlight(v bool) { r.highlight = v }

// SetShowLoader toggles the streaming "generating" animation. Off swaps the
// braille loader/caret for a static ⬢ marker so the row still signals
// activity without motion.
func (r *StreamRenderer) SetShowLoader(v bool) { r.showLoader = v }

// hl returns content with chroma highlighting using the lexer matching
// path. Returns the input unchanged when r.highlight is off or no lexer
// resolves. Trailing newlines are preserved (chroma appends a reset).
func (r *StreamRenderer) hl(content, path string) string {
	if !r.highlight {
		return content
	}
	return HighlightCode(content, langFromPath(path))
}

// hlLang highlights with an explicit lexer name (e.g. "bash", "diff") when
// no path is available. Same off-switch + safe-fallback semantics as hl.
func (r *StreamRenderer) hlLang(content, lang string) string {
	if !r.highlight {
		return content
	}
	return HighlightCode(content, lang)
}

// diffSign renders one diff line as `<prefix-styled> <highlighted-content>`.
// When highlight is off, falls back to wrapping the whole line in the prefix
// style — preserves the pre-feature look exactly.
func (r *StreamRenderer) diffSign(sign string, content string, path string, signStyle lipgloss.Style) string {
	if !r.highlight {
		return signStyle.Render(sign + " " + content)
	}
	return signStyle.Render(sign) + " " + HighlightCode(content, langFromPath(path))
}

// SetCompact toggles compact mode. When true, RenderMessage and friends emit
// the dense pre-pi layout (no outer gutter, no inter-turn blank line, no
// user bg-tint, no OSC 133 prompt zones). Default false = clean mode.
func (r *StreamRenderer) SetCompact(v bool) { r.compact = v }

// argsSummary returns the rune budget for tool-input summaries.
func (r *StreamRenderer) argsSummary() int {
	if r.verbose {
		return argsSummaryVerbose
	}
	return argsSummaryCompact
}

// argsBudget computes a width-aware rune budget for the tool-card args
// summary. Card looks like `<name>  <key>: <value>`; we give the value
// the rest of the row instead of a fixed 40 chars — the old cap was hiding
// bash commands like `cmd: find /Users/userX/web/b…` before the
// actual interesting flags showed up. Floor at argsSummaryCompact so narrow
// terminals still get something readable; verbose mode raises the ceiling
// but still respects width.
func (r *StreamRenderer) argsBudget(toolName string) int {
	// name + 2 spaces + key (~9 "command: ") = ~12 overhead
	const overhead = 12
	b := r.width - overhead - len(toolName)
	if b < argsSummaryCompact {
		b = argsSummaryCompact
	}
	if r.verbose && b < argsSummaryVerbose {
		b = argsSummaryVerbose
	}
	return b
}

// previewLines returns the per-tool-result row budget. Verbose returns -1
// meaning "no cap" — the caller skips truncation when negative.
func (r *StreamRenderer) previewLines() int {
	if r.verbose {
		return -1
	}
	return previewLinesCompact
}

// NewStreamRenderer builds a renderer. width is the wrap target; pass 0 for
// glamour's default (80). Loader style is taken from BEE_LOADER (swarm /
// comb / dance / drip); unset / "random" → random pick at construction.
func NewStreamRenderer(styles Styles, width int) *StreamRenderer {
	if width <= 0 {
		width = 80
	}
	// WithAutoStyle would query the terminal via OSC 11; the reply leaks
	// into bubbletea's input pipe in altscreen mode and ends up typed in
	// the textinput. Pick standard style explicitly from the already-set
	// lipgloss flag (cmd/bee/tui.go decides this pre-program from
	// BEE_THEME / COLORFGBG).
	gStyle := "dark"
	if !lipgloss.HasDarkBackground() {
		gStyle = "light"
	}
	// WithPreservedNewLines keeps model's intentional line breaks (lists,
	// short prose lines) instead of collapsing them into one paragraph.
	// WithEmoji expands `:sparkles:` shortcodes — cheap delight.
	// Wrap at width-2 so the 2-col continuation indent we add downstream
	// doesn't push the longest glamour line past the terminal right edge.
	wrap := width - 2
	if wrap < 20 {
		wrap = width
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(gStyle),
		glamour.WithWordWrap(wrap),
		glamour.WithPreservedNewLines(),
		glamour.WithEmoji(),
	)
	if err != nil {
		// fallback: nil md renderer means plain text.
		r = nil
	}
	return &StreamRenderer{
		styles:       styles,
		md:           r,
		width:        width,
		showThoughts: true,
		highlight:    true,
		showLoader:   true,
		loaderStyle:  ParseLoaderStyle(envLoaderStyle()),
	}
}








