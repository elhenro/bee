package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"

	"github.com/elhenro/bee/internal/types"
)

// argsSummaryCompact / argsSummaryVerbose cap tool-input json so cards stay
// one-liners; verbose mode lets longer key paths through.
const argsSummaryCompact = 40
const argsSummaryVerbose = 120

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
	mdWrap       int                           // wrap width md renders at
	mdTables     map[int]*glamour.TermRenderer // per table width, see renderMarkdown
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
	// showCallDetail prints the whole call under a labeled call card.
	showCallDetail bool
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
	// last rendered message was a tool card: the next one stacks tight
	afterTool bool
	// the last tool message cut rows from its output; ctrl+o shows the rest
	clipped bool
	// reply already printed its marker (progressive flush), no second ⬢
	cont bool
	// pulse phase of in-flight call bullets
	pendingFrame int
	// compactingVariant selects one of the three /compact swarm animations.
	// Picked at frame 0 of each compacting run, sticks for the duration.
	compactingVariant int
	// loaderStats feeds the default token-stream loader the live turn figures
	// (input tokens, output chars, throughput, seed). Set by the view each
	// render before the loader/strip is painted.
	loaderStats LoaderStats

	// streaming render cache. View() is called on every key/tick/window event
	// — often multiple times per frame for the same partial+frame. Cache the
	// last rendered output so repeat calls short-circuit the re-render. Width
	// is not a key because the renderer is reconstructed on resize.
	cachedStreamPartial string
	cachedStreamFrame   int
	cachedStreamOutput  string
	cachedStreamValid   bool

	// streamMarkdown cache: render of the last settled prefix
	settledSrc string
	settledOut string
}

// invalidateStreamCache drops the cached streaming render so the next
// RenderStreaming call rebuilds from source. Called by setters that change
// rendering output and from SetCompact (gutter changes layout).
func (r *StreamRenderer) invalidateStreamCache() {
	r.cachedStreamValid = false
	r.settledSrc, r.settledOut = "", ""
}

// next text continues a reply whose head is already in scrollback
func (r *StreamRenderer) SetContinuation(v bool) {
	if r.cont != v {
		r.cont = v
		r.invalidateStreamCache()
	}
}

// SetLoaderStyle picks which pre-token loader animation to render.
func (r *StreamRenderer) SetLoaderStyle(s LoaderStyle) {
	r.loaderStyle = s
	r.invalidateStreamCache()
}

// SetLoaderStats feeds the default token-stream loader the live turn
// figures. No-op for pinned named styles, which ignore the stats.
func (r *StreamRenderer) SetLoaderStats(s LoaderStats) { r.loaderStats = s }

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

// SetShowCallDetail toggles the full-call row under tool calls that carry a
// model-written label. Failed calls show it regardless.
func (r *StreamRenderer) SetShowCallDetail(v bool) { r.showCallDetail = v }

// SetHighlight toggles chroma syntax-highlighting across diff/file/bash
// previews. Off returns raw content; on (default) emits ANSI-colored tokens.
func (r *StreamRenderer) SetHighlight(v bool) { r.highlight = v }

// SetShowLoader toggles the streaming "generating" animation. Off swaps the
// braille loader/caret for a static ⬢ marker so the row still signals
// activity without motion.
func (r *StreamRenderer) SetShowLoader(v bool) {
	r.showLoader = v
	r.invalidateStreamCache()
}

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

// preserveBgAcrossResets rewrites bare `\x1b[0m` SGR resets inside s so they
// reset every attribute *except* the background color. Chroma terminates each
// highlighted token with a full reset, which kills any outer Background()
// applied by lipgloss — leaving gaps between syntax-colored tokens on a
// diff row. Replacing `0m` with `22;23;24;27;29;39m` clears bold/italic/
// underline/inverse/strikethrough + fg color but leaves bg intact, so the
// row-wide tint stays continuous.
func preserveBgAcrossResets(s string) string {
	return strings.ReplaceAll(s, "\x1b[0m", "\x1b[22;23;24;27;29;39m")
}

// SetCompact toggles compact mode. When true, RenderMessage and friends emit
// the dense pre-pi layout (no outer gutter, no inter-turn blank line, no
// user bg-tint, no OSC 133 prompt zones). Default false = clean mode.
func (r *StreamRenderer) SetCompact(v bool) {
	r.compact = v
	r.invalidateStreamCache()
}

// rune budget for a call row's args: the rest of the line, so a bash
// command shows its real flags; floors at argsSummaryCompact, verbose
// raises the ceiling
func (r *StreamRenderer) argsBudget(toolName string) int {
	// gutter + bullet + space + two-space gap + right margin
	const overhead = 6
	b := max(r.width-overhead-len(toolTitle(toolName)), argsSummaryCompact)
	if r.verbose {
		b = max(b, argsSummaryVerbose)
	}
	return b
}

// RenderMessageDetail renders m with the tool-output cap forced full (no cap)
// or collapsed (compact preview), independent of the persistent verbose flag.
// Used by the ctrl+o re-print toggle. Restores verbose afterward.
func (r *StreamRenderer) RenderMessageDetail(m types.Message, full bool) string {
	prev, prevTool := r.verbose, r.afterTool
	r.verbose, r.afterTool = full, false
	out := r.RenderMessage(m)
	r.verbose, r.afterTool = prev, prevTool
	return out
}

// NewStreamRenderer builds a renderer. width is the wrap target; pass 0 for
// glamour's default (80). Loader style is taken from BEE_LOADER (see
// ParseLoaderStyle); unset → the busy-row spinner, "random" → random pick.
func NewStreamRenderer(styles Styles, width int) *StreamRenderer {
	if width <= 0 {
		width = 80
	}
	// gutter + marker column + right margin
	wrap := max(width-4, minTextWidth)
	// a nil md renderer means plain text
	r := newMarkdown(wrap)
	return &StreamRenderer{
		styles:       styles,
		md:           r,
		mdWrap:       wrap,
		width:        width,
		showThoughts: true,
		highlight:    true,
		showLoader:   true,
		loaderStyle:  ParseLoaderStyle(os.Getenv("BEE_LOADER")),
	}
}
