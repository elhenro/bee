package tui

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

// inlineShellPrefix marks a message body as an inline `!cmd` / `!!cmd`
// shell-exec record. Stored as the first line so the renderer can route to
// renderInlineShell. Plain text so the LLM (which sees this in context) can
// still understand it as a shell invocation block.
const inlineShellPrefix = "[shell exit="

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

// LoaderStyle selects which braille animation runs while waiting on the
// first token. Default is the phased pipeline (chill → swarm → orbit →
// ripple → starfield) which evolves the longer the wait runs. Named
// values pin a single painter.
type LoaderStyle int

const (
	LoaderStyleDefault   LoaderStyle = iota // phased: pulse → swarm → orbit → ripple → starfield
	LoaderStylePulse                        // centered bee, gentle wing flap
	LoaderStyleSwarm                        // multi-particle swarm, scales with width
	LoaderStyleWave                         // layered sine waves
	LoaderStyleComet                        // bright head + decaying tail
	LoaderStyleHex                          // rotating hexagonal outline
	LoaderStyleRipple                       // concentric ellipses expanding
	LoaderStyleRain                         // falling drops
	LoaderStyleOrbit                        // particles on elliptical orbit
	LoaderStyleBreath                       // bar expanding/contracting from center
	LoaderStyleStars                        // drifting starfield
	LoaderStyleForage                       // bees leave hive, drift, return
	LoaderStyleFigure8                      // waggle-dance lemniscate
	LoaderStyleVortex                       // 3 nested rotating rings
	LoaderStyleGust                         // swarm in wind with gust spikes
	LoaderStyleScatter                      // alarm dispersal + regroup
	LoaderStyleFlock                        // 3 cohesive bee clusters
	LoaderStyleDNA                          // double helix
	LoaderStyleMatrix                       // variable-speed vertical streams
	LoaderStyleHeartbeat                    // EKG flatline + spike
	LoaderStyleLightning                    // sudden bolt + decay
	LoaderStyleSnake                        // segments chasing head
	LoaderStyleFireworks                    // radiating bursts with gravity
	LoaderStyleDrunk                        // bee wobbles, drank fermented honey
	LoaderStyleJar                          // bee trapped, bouncing off jar walls
	LoaderStyleConga                        // bee conga line, undulating march
	LoaderStyleQueen                        // queen + 4 attendants procession
	LoaderStyleDrip                         // fat honey droplet + accumulating pool
	LoaderStyleMarathon                     // bees racing toward finish line
)

// ParseLoaderStyle maps BEE_LOADER values to a style. Unset / "random" /
// unknown → phased default. Legacy aliases (swarm/comb/dance/drip) map
// to their nearest braille equivalent so old configs keep working.
func ParseLoaderStyle(s string) LoaderStyle {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default", "phased", "auto":
		return LoaderStyleDefault
	case "swarm", "bee", "bees":
		return LoaderStyleSwarm
	case "pulse", "bee-flap":
		return LoaderStylePulse
	case "wave", "comb": // comb is a legacy alias from the old ASCII era
		return LoaderStyleWave
	case "comet", "trail":
		return LoaderStyleComet
	case "hex", "hexagon":
		return LoaderStyleHex
	case "ripple", "rings":
		return LoaderStyleRipple
	case "rain", "drip", "honey": // drip/honey are legacy aliases
		return LoaderStyleRain
	case "orbit", "dance", "waggle": // dance/waggle are legacy aliases
		return LoaderStyleOrbit
	case "breath", "breathe":
		return LoaderStyleBreath
	case "stars", "starfield", "cosmic":
		return LoaderStyleStars
	case "forage", "hive", "foraging":
		return LoaderStyleForage
	case "figure8", "fig8", "lemniscate":
		return LoaderStyleFigure8
	case "vortex", "spin", "tornado":
		return LoaderStyleVortex
	case "gust", "wind", "breeze":
		return LoaderStyleGust
	case "scatter", "alarm", "disperse":
		return LoaderStyleScatter
	case "flock", "cluster", "boids":
		return LoaderStyleFlock
	case "dna", "helix", "double-helix":
		return LoaderStyleDNA
	case "matrix", "cascade", "rain2":
		return LoaderStyleMatrix
	case "heartbeat", "ekg", "pulse-line":
		return LoaderStyleHeartbeat
	case "lightning", "bolt", "strike":
		return LoaderStyleLightning
	case "snake", "serpent", "chase":
		return LoaderStyleSnake
	case "fireworks", "burst", "explosion":
		return LoaderStyleFireworks
	case "drunk", "tipsy", "fermented":
		return LoaderStyleDrunk
	case "jar", "trapped", "stuck":
		return LoaderStyleJar
	case "conga", "line", "party":
		return LoaderStyleConga
	case "queen", "royal", "procession":
		return LoaderStyleQueen
	case "drip2", "droplet", "honey-drop":
		return LoaderStyleDrip
	case "marathon", "race", "finish":
		return LoaderStyleMarathon
	case "random", "rand", "?":
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		return LoaderStyle(1 + r.Intn(len(brailleNamedPainters)))
	default:
		return LoaderStyleDefault
	}
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
	// toolUses indexes tool calls by ID so renderToolResult can recover the
	// originating cmd/args (e.g. surface the failed bash command in place of
	// the bare "exit N" preview). Populated lazily as RenderMessage walks
	// each turn.
	toolUses map[string]types.ToolUse
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
		loaderStyle:  ParseLoaderStyle(envLoaderStyle()),
	}
}

// envLoaderStyle reads BEE_LOADER lazily so tests can override per-call by
// setting the env var before NewStreamRenderer. Indirected for clarity.
func envLoaderStyle() string { return os.Getenv("BEE_LOADER") }

// formatInlineShell builds the stored text for an inline shell record:
//
//	[shell exit=N]
//	$ <cmd>
//	<output>
//
// The first-line marker is the parser hook for renderInlineShell; the rest
// reads naturally so the LLM (which sees this in context for !cmd) can
// understand it as a shell exec block.
func formatInlineShell(cmd, output string, isErr bool) string {
	exit := 0
	if isErr {
		exit = 1
	}
	out := strings.TrimRight(output, "\n")
	if out == "" {
		return fmt.Sprintf("%s%d]\n$ %s", inlineShellPrefix, exit, cmd)
	}
	return fmt.Sprintf("%s%d]\n$ %s\n%s", inlineShellPrefix, exit, cmd, out)
}

// parseInlineShell extracts cmd, output, and exit-status from a message body
// produced by formatInlineShell. Returns ok=false if the text doesn't match.
func parseInlineShell(text string) (cmd, output string, isErr, ok bool) {
	if !strings.HasPrefix(text, inlineShellPrefix) {
		return "", "", false, false
	}
	rest := text[len(inlineShellPrefix):]
	closeIdx := strings.Index(rest, "]\n")
	if closeIdx < 0 {
		return "", "", false, false
	}
	exitStr := rest[:closeIdx]
	body := rest[closeIdx+2:]
	// expect first body line to be "$ <cmd>"
	nl := strings.IndexByte(body, '\n')
	var cmdLine string
	if nl < 0 {
		cmdLine = body
		output = ""
	} else {
		cmdLine = body[:nl]
		output = body[nl+1:]
	}
	if !strings.HasPrefix(cmdLine, "$ ") {
		return "", "", false, false
	}
	cmd = cmdLine[2:]
	isErr = exitStr != "0"
	return cmd, output, isErr, true
}

// renderInlineShell styles an inline shell record:
//
//	$ ls            (honey-bold prompt + honey-bold cmd)
//	▎ file1.go      (honey rail + body output, ANSI-stripped, indented)
//	▎ file2.go
//
// Glyph is replaced by the `$` prompt; output spans full width without the
// 4-line preview clip tool cards use. Whole card is honey-themed so it reads
// at a glance as "this came from !cmd / !!cmd" rather than blending into
// regular prose.
func (r *StreamRenderer) renderInlineShell(cmd, output string, isErr bool) string {
	honey := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	rail := lipgloss.NewStyle().Foreground(accentHoney).Render("▎")
	header := honey.Render("$ " + cmd) // single styled span keeps coloring contiguous
	clean := ansi.Strip(strings.TrimRight(output, "\n"))
	if clean == "" {
		return header
	}
	bodyStyle := r.styles.ToolPrev
	if isErr {
		bodyStyle = r.styles.Error
	}
	// prefix each output line with a honey rail so the block reads as a card.
	lines := strings.Split(clean, "\n")
	for i, ln := range lines {
		lines[i] = rail + " " + bodyStyle.Render(ln)
	}
	return header + "\n" + strings.Join(lines, "\n")
}

// isNudgeMessage reports whether m is a synthetic recovery nudge emitted by
// the loop (reasoning-only stall → injected user turn whose text starts with
// the `[nudge]` marker). Used to suppress them from scrollback unless the
// `show_nudges` setting flips it on.
func isNudgeMessage(m types.Message) bool {
	if m.Role != types.RoleUser {
		return false
	}
	tb := firstTextBlock(m)
	if tb == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(tb.Text, " \t"), "[nudge]")
}

// firstTextBlock returns the first BlockText in m (nil if none). Used to keep
// inline-shell detection working when an image companion is staged.
func firstTextBlock(m types.Message) *types.ContentBlock {
	for i := range m.Content {
		if m.Content[i].Type == types.BlockText {
			return &m.Content[i]
		}
	}
	return nil
}

// RenderMessage formats a single message for the scrollback. Glyph and the
// first body line share one row; continuation lines indent under the body
// column. Dense by design — `▸ yo yo` reads as one unit.
//
// Each ContentBlock renders independently; results are right-trimmed and
// joined on a single newline so neighbouring blocks never collide on the
// same row (the original concat-without-separator caused text to share a
// line with the following tool card). Interior blank runs are collapsed
// so glamour padding or stray `\n\n\n` from the model can't spill
// hundreds of empty rows into terminal scrollback.
func (r *StreamRenderer) RenderMessage(m types.Message) string {
	// hide synthetic [nudge] user messages unless explicitly enabled. the
	// loop still injects + persists these turns; we only suppress the visual
	// row so the user isn't distracted by recovery prods.
	if !r.showNudges && isNudgeMessage(m) {
		return ""
	}
	// inline shell records (from `!cmd` / `!!cmd`) get a dedicated styled
	// rendering instead of the role-glyph + prose layout.
	if tb := firstTextBlock(m); tb != nil {
		if cmd, out, isErr, ok := parseInlineShell(tb.Text); ok {
			body := r.renderInlineShell(cmd, out, isErr)
			if r.compact {
				return body
			}
			return "\n" + applyGutter(body)
		}
	}
	glyph := r.roleGlyph(m.Role)

	// pre-pass: index tool-use blocks so a later renderToolResult can look up
	// the originating command (used by the bash error-card path).
	for _, b := range m.Content {
		if b.Type == types.BlockToolUse && b.Use != nil {
			if r.toolUses == nil {
				r.toolUses = make(map[string]types.ToolUse)
			}
			r.toolUses[b.Use.ID] = *b.Use
		}
	}

	parts := make([]string, 0, len(m.Content))
	for _, b := range m.Content {
		var rendered string
		switch b.Type {
		case types.BlockText:
			rendered = r.renderText(b.Text)
		case types.BlockThinking:
			rendered = r.renderThinking(b.Text)
		case types.BlockToolUse:
			if b.Use != nil {
				rendered = r.renderToolUse(*b.Use)
			}
		case types.BlockToolResult:
			if b.Result != nil {
				rendered = r.renderToolResult(*b.Result)
			}
		}
		rendered = strings.TrimRight(rendered, "\n")
		if rendered == "" {
			continue
		}
		parts = append(parts, rendered)
	}

	bodyStr := collapseBlankRuns(strings.Trim(strings.Join(parts, "\n"), "\n"))

	var rendered string
	if m.Role == types.RoleUser {
		rail := r.styles.UserRail.Render("┃")
		// drop the role glyph: rail + bold-blue body is enough to anchor
		// the turn and reads like a quoted prompt. body color matches the
		// rail so the whole block reads as one unit.
		bodyDecorate := func(s string) string { return r.styles.UserBody.Render(s) }
		if bodyStr == "" {
			rendered = rail
		} else {
			bodyLines := strings.Split(bodyStr, "\n")
			bodyLines[0] = rail + " " + bodyDecorate(bodyLines[0])
			for i := 1; i < len(bodyLines); i++ {
				bodyLines[i] = rail + " " + bodyDecorate(bodyLines[i])
			}
			rendered = strings.Join(bodyLines, "\n")
		}
	} else if m.Role == types.RoleAssistant {
		// assistant turns render without a role glyph — the user prompt
		// above already anchors the conversation, and stripping the prefix
		// gives prose full column width without a leading hex distraction.
		if bodyStr == "" {
			return ""
		}
		rendered = bodyStr
	} else if bodyStr == "" {
		rendered = glyph
	} else if glyph == "" {
		rendered = bodyStr
	} else {
		rendered = glyph + " " + indentContinuation(bodyStr, "  ")
	}

	// compact mode skips the spacing layer entirely (no gutter, no OSC
	// 133 zones, no leading blank). Useful on small terminals or for users
	// who prefer a denser layout.
	if r.compact {
		return rendered
	}

	rendered = applyGutter(rendered)

	// OSC 133 wraps finalized user/assistant turns. Tool messages stay
	// un-wrapped — they're sub-content the terminal shouldn't bookmark.
	if m.Role == types.RoleUser || m.Role == types.RoleAssistant {
		rendered = osc133Start + rendered + osc133End
	}

	// Leading blank line above every message — `Spacer(1)` so each
	// turn breathes vertically. tea.Println adds the trailing \n.
	return "\n" + rendered
}

// collapseBlankRuns squeezes 2+ consecutive blank/whitespace-only lines into
// a single blank. Markdown rendering (glamour) and the model itself both
// like to pad with multiple blanks; in scrollback every blank is a wasted
// row, so cap the run at one. A line is "blank" iff every rune is space or
// tab (after ANSI strip via the surrounding pipeline). Preserves content
// lines verbatim — only whitespace runs get touched.
func collapseBlankRuns(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		isBlank := strings.TrimSpace(l) == ""
		if isBlank && blank {
			continue
		}
		out = append(out, l)
		blank = isBlank
	}
	return strings.Join(out, "\n")
}

// roleGlyph returns the single-glyph role marker. Label dropped — colored
// glyph alone disambiguates speaker without eating a row.
func (r *StreamRenderer) roleGlyph(role types.Role) string {
	switch role {
	case types.RoleUser:
		return r.styles.RoleYou.Render("▸")
	case types.RoleAssistant:
		return r.styles.RoleBee.Render("⬢")
	case types.RoleTool:
		return ""
	default:
		return r.styles.Dim.Render("·")
	}
}

// indentContinuation leaves the first line as-is and prefixes every later
// line with indent. Used so multi-line bodies align under the body column,
// not the role glyph.
func indentContinuation(s, indent string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

// RenderStreaming returns the partial-text view while the model emits deltas.
// A subtle ▍ caret tracks the cursor. frame drives the pre-token loader
// animation; once partial is non-empty the animation gives way to text.
//
// The partial is rendered as RAW text (not glamour-markdown). Re-rendering a
// growing buffer through glamour on every delta reflows word-wrap and shifts
// indent as markdown tokens (`-`, `*`, ```` ``` ````) come into being mid-
// stream — visually the text "jumps" and indentation breaks. Markdown styling
// is applied once the turn finishes in RenderMessage. Continuation lines are
// indented 2 cols so they align under the body column, not the role glyph.
func (r *StreamRenderer) RenderStreaming(partial string, frame int) string {
	if partial == "" {
		// no right caret while loading — keeps the row visually minimal.
		// blank line above so loader breathes; user prompt isn't squashed
		// against animation. Loader animation alone signals "bee working" —
		// the prefix ⬢ was redundant with the animated braille payload.
		head := r.renderLoader(frame)
		if r.compact {
			return "\n" + head
		}
		return "\n" + outerGutter + head
	}
	caret := r.styles.Dim.Render("▍")
	// trim trailing whitespace so the caret sits flush with the last visible
	// char instead of floating on an indented blank line under the prose.
	trimmed := strings.TrimRight(partial, " \t\n")
	body := trimmed + " " + caret
	if r.compact {
		return body
	}
	return applyGutter(body)
}

// pulseStyle picks the loader color for this frame — alternates between
// accent and dim every 6 ticks so the art breathes.
func (r *StreamRenderer) pulseStyle(nf int) lipgloss.Style {
	if (nf/6)%2 == 0 {
		return r.styles.RoleBee
	}
	return lipgloss.NewStyle().Foreground(fgSquid)
}

// loaderCells returns the canvas width in braille cells. The loader sits
// after a leading glyph + space (and a 1-col outer gutter in clean mode),
// so we deduct those before clamping. Tiny terminals fall back to the
// minimum cell count.
func (r *StreamRenderer) loaderCells() int {
	prefix := 3 // glyph + 2 spaces
	if !r.compact {
		prefix++ // outer gutter
	}
	cells := r.width - prefix
	if cells < brailleLoaderMinCells {
		cells = brailleLoaderMinCells
	}
	return cells
}

// renderLoader draws the streaming-wait animation. Every style is a
// braille painter; the default style is a phased painter that escalates
// over time. Output is always single-row, ~r.width chars wide.
func (r *StreamRenderer) renderLoader(frame int) string {
	nf := frame
	if nf < 0 {
		nf = -nf
	}
	cells := r.loaderCells()
	painter := r.painterFor(nf)
	art := painter(nf, cells)
	return r.pulseStyle(nf).Render(art)
}

// painterFor resolves the active loader style to a braille painter. The
// default style returns the phase-appropriate painter for the current
// frame; named styles return their pinned painter.
func (r *StreamRenderer) painterFor(nf int) func(frame, cells int) string {
	switch r.loaderStyle {
	case LoaderStylePulse:
		return renderBraillePulse
	case LoaderStyleSwarm:
		return renderBrailleSwarm
	case LoaderStyleWave:
		return renderBrailleWave
	case LoaderStyleComet:
		return renderBrailleComet
	case LoaderStyleHex:
		return renderBrailleHex
	case LoaderStyleRipple:
		return renderBrailleRipple
	case LoaderStyleRain:
		return renderBrailleRain
	case LoaderStyleOrbit:
		return renderBrailleOrbit
	case LoaderStyleBreath:
		return renderBrailleBreath
	case LoaderStyleStars:
		return renderBrailleStarfield
	case LoaderStyleForage:
		return renderBrailleForage
	case LoaderStyleFigure8:
		return renderBrailleFigure8
	case LoaderStyleVortex:
		return renderBrailleVortex
	case LoaderStyleGust:
		return renderBrailleGust
	case LoaderStyleScatter:
		return renderBrailleScatter
	case LoaderStyleFlock:
		return renderBrailleFlock
	case LoaderStyleDNA:
		return renderBrailleDNA
	case LoaderStyleMatrix:
		return renderBrailleMatrix
	case LoaderStyleHeartbeat:
		return renderBrailleHeartbeat
	case LoaderStyleLightning:
		return renderBrailleLightning
	case LoaderStyleSnake:
		return renderBrailleSnake
	case LoaderStyleFireworks:
		return renderBrailleFireworks
	case LoaderStyleDrunk:
		return renderBrailleDrunk
	case LoaderStyleJar:
		return renderBrailleJar
	case LoaderStyleConga:
		return renderBrailleConga
	case LoaderStyleQueen:
		return renderBrailleQueen
	case LoaderStyleDrip:
		return renderBrailleDrip
	case LoaderStyleMarathon:
		return renderBrailleMarathon
	default:
		return braillePhaseFor(nf)
	}
}

// RenderCompacting draws the /compact-specific loader: a braille bar
// that folds inward from both edges to the center, then bounces back.
// Reads as memory being squeezed into a summary.
func (r *StreamRenderer) RenderCompacting(frame int) string {
	nf := frame
	if nf < 0 {
		nf = -nf
	}
	cells := r.loaderCells()
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// triangle wave: edges → center → edges over 2*half frames.
	half := w / 2
	if half < 4 {
		half = 4
	}
	step := (nf / 2) % (half * 2)
	gap := step
	if gap > half {
		gap = half*2 - step
	}
	// fill from each edge inward up to (half - gap) pixels.
	fill := half - gap
	if fill < 0 {
		fill = 0
	}
	y := braillePxH / 2
	for x := 0; x < fill; x++ {
		c.SetPixel(x, y, true)
		c.SetPixel(w-1-x, y, true)
		// inner glow rows for thickness
		if x < fill-1 {
			c.SetPixel(x, y-1, true)
			c.SetPixel(w-1-x, y+1, true)
		}
	}
	// single bright center dot — always lit so the bar never goes dark.
	c.SetPixel(half, y, true)
	body := r.styles.RoleBee.Render("⬢") + " " + r.pulseStyle(nf).Render(c.ToBraille())
	if r.compact {
		return body
	}
	return outerGutter + body
}

func (r *StreamRenderer) renderText(s string) string {
	if r.md == nil || s == "" {
		return s
	}
	// short-circuit: glamour's document margin and surrounding blank lines
	// turn "Yo." into "  Yo." sandwiched in blanks. Plain text gets returned
	// raw; only invoke glamour when there's actual markdown to style.
	if !needsMarkdown(s) {
		return s
	}
	out, err := r.md.Render(s)
	if err != nil {
		return s
	}
	return dedent(strings.Trim(out, "\n"))
}

// needsMarkdown is a crude marker check — fences, inline code, leading list/
// heading/quote chars, numbered lists, horizontal rules, links, or bold/em
// emphasis. Catches prose that benefits from glamour; lets bare chat replies
// through untouched so a "yo." doesn't get framed in document margins.
func needsMarkdown(s string) bool {
	if strings.Contains(s, "```") || strings.Contains(s, "`") {
		return true
	}
	if strings.Contains(s, "**") || strings.Contains(s, "__") {
		return true
	}
	// inline link: [text](url) — cheap substring check covers the
	// well-formed case; false positives on prose are harmless.
	if strings.Contains(s, "](") {
		return true
	}
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimLeft(ln, " \t")
		if t == "" {
			continue
		}
		switch t[0] {
		case '#', '>', '-', '*', '|':
			return true
		}
		// numbered list: `1. `, `12. ` — digits then `. ` or `) `.
		if t[0] >= '0' && t[0] <= '9' {
			i := 0
			for i < len(t) && t[i] >= '0' && t[i] <= '9' {
				i++
			}
			if i < len(t)-1 && (t[i] == '.' || t[i] == ')') && t[i+1] == ' ' {
				return true
			}
		}
		// horizontal rule: --- *** ___
		if len(t) >= 3 {
			c := t[0]
			if c == '-' || c == '*' || c == '_' {
				all := true
				for j := 0; j < len(t); j++ {
					if t[j] != c && t[j] != ' ' {
						all = false
						break
					}
				}
				if all {
					return true
				}
			}
		}
	}
	return false
}

// dedent strips the common leading-space prefix from every non-blank line and
// trims trailing spaces. Glamour's standard style adds a document margin (we
// don't want it eating columns) and right-pads lines for table/quote bg
// fill (which paints color bands at the right edge of the terminal). Both
// look like junk in our compact layout.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	// dedent leading spaces uses *visible* leading width; ANSI escapes
	// inserted by glamour at the start of a line (e.g. color reset) must
	// not count toward the indent. ansi.Strip via the surrounding pipeline
	// is too aggressive — we still want the escapes preserved. So we strip
	// in a working copy just for the measurement.
	min := -1
	for _, l := range lines {
		visible := ansi.Strip(l)
		if strings.TrimSpace(visible) == "" {
			continue
		}
		n := len(visible) - len(strings.TrimLeft(visible, " "))
		if min < 0 || n < min {
			min = n
		}
	}
	if min < 0 {
		min = 0
	}
	for i, l := range lines {
		if min > 0 {
			l = stripLeadingSpacesPreservingANSI(l, min)
		}
		lines[i] = trimTrailingVisibleSpaces(l)
	}
	return strings.Join(lines, "\n")
}

// trimTrailingVisibleSpaces drops trailing whitespace from l even when the
// whitespace is wrapped in per-span ANSI color spans. Glamour right-pads
// every line to the wrap width and emits `\x1b[38;5;252m \x1b[0m` per space,
// so plain TrimRight sees `m` at the end and does nothing.
//
// Algorithm: split l into [span | visible] runs by scanning ANSI escapes.
// Drop trailing runs whose visible content is empty or whitespace-only,
// keeping the preceding ANSI escapes that bracket non-whitespace content.
func trimTrailingVisibleSpaces(l string) string {
	if l == "" {
		return l
	}
	// find offsets of each ANSI escape and the visible bytes between them.
	type seg struct {
		isAnsi bool
		s, e   int // [s, e)
	}
	segs := make([]seg, 0, 16)
	i := 0
	for i < len(l) {
		if l[i] == 0x1b && i+1 < len(l) && l[i+1] == '[' {
			j := i + 2
			for j < len(l) && (l[j] < 0x40 || l[j] > 0x7e) {
				j++
			}
			if j < len(l) {
				j++
			}
			segs = append(segs, seg{isAnsi: true, s: i, e: j})
			i = j
			continue
		}
		// visible run until next ESC
		j := i
		for j < len(l) && l[j] != 0x1b {
			j++
		}
		segs = append(segs, seg{isAnsi: false, s: i, e: j})
		i = j
	}
	// walk from the end dropping visible runs that are blank-only; ansi
	// runs between blanks are dropped too. Stop once we hit a non-blank
	// visible run.
	cut := len(l)
	for k := len(segs) - 1; k >= 0; k-- {
		sg := segs[k]
		if sg.isAnsi {
			cut = sg.s
			continue
		}
		blank := true
		for p := sg.s; p < sg.e; p++ {
			if l[p] != ' ' && l[p] != '\t' {
				blank = false
				break
			}
		}
		if blank {
			cut = sg.s
			continue
		}
		// keep this visible run intact
		break
	}
	return l[:cut]
}

// stripLeadingSpacesPreservingANSI removes up to n visible leading spaces
// from l while leaving ANSI escape sequences intact. Glamour emits a color
// reset before the document margin, so a naive l[n:] would slice mid-escape
// and corrupt the rest of the line.
func stripLeadingSpacesPreservingANSI(l string, n int) string {
	if n <= 0 || l == "" {
		return l
	}
	var b strings.Builder
	b.Grow(len(l))
	removed := 0
	i := 0
	for i < len(l) {
		if l[i] == 0x1b && i+1 < len(l) && l[i+1] == '[' {
			// copy through to terminator byte (alpha in 0x40-0x7e)
			j := i + 2
			for j < len(l) && (l[j] < 0x40 || l[j] > 0x7e) {
				j++
			}
			if j < len(l) {
				j++
			}
			b.WriteString(l[i:j])
			i = j
			continue
		}
		if l[i] == ' ' && removed < n {
			removed++
			i++
			continue
		}
		b.WriteString(l[i:])
		break
	}
	return b.String()
}

// renderToolUse renders the compact card: name  args-summary. Lilac
// name so tool activity reads as a distinct lane vs honey-AI prose and
// blue-user input. Args are omitted entirely (no trailing whitespace)
// when the tool was called with no input.
//
// File-mutation tools (edit / hashline_edit / apply_patch / write) skip
// the json summary and instead drop a colored diff card under the header
// so the reader sees the change itself, not `{"new":"...","old":"..."}`.
func (r *StreamRenderer) renderToolUse(u types.ToolUse) string {
	name := r.styles.ToolName.Render(u.Name)
	if header, body, ok := r.renderEditPreview(u); ok {
		head := fmt.Sprintf("%s  %s", name, header)
		if body == "" {
			return head + "\n"
		}
		return head + "\n" + body + "\n"
	}
	args := summarizeToolArgs(u.Name, u.Input, r.argsBudget(u.Name))
	if args == "" {
		return fmt.Sprintf("%s\n", name)
	}
	return fmt.Sprintf("%s  %s\n", name, r.styles.ToolArgs.Render(args))
}

// summarizeToolArgs routes per-tool pretty summaries before falling back to
// the generic json/single-key path. bash gets its `command` pulled out bare
// (tool name already says "bash", so `command:` prefix would be redundant)
// with cwd folded in as a compact `· <cwd>` suffix when set.
func summarizeToolArgs(toolName string, in map[string]any, budget int) string {
	if toolName == "bash" {
		if s, ok := summarizeBash(in, budget); ok {
			return s
		}
	}
	return summarizeArgs(in, budget)
}

// summarizeBash renders a bash tool call as `<command> · <cwd>` (cwd
// suffix dropped when empty). Command paths shorten inline so cd targets
// don't waste card width on /Users/<name>/projects/... prefixes. Returns
// (_, false) when the input has no `command` field so the caller falls
// back to the generic json summary path.
func summarizeBash(in map[string]any, budget int) (string, bool) {
	cmd, ok := in["command"].(string)
	if !ok || cmd == "" {
		return "", false
	}
	out := shortenPathsInline(cmd)
	if cwd, ok := in["cwd"].(string); ok && cwd != "" {
		out += "  · " + shortenPath(cwd)
	}
	return truncateRunes(out, budget), true
}

// diffPreviewLinesCompact caps body height for compact-mode edit previews.
// Verbose mode lets every line through (mirrors previewLines() semantics).
const diffPreviewLinesCompact = 8

// renderEditPreview turns a file-mutation tool call into a pretty
// `path` header + colored diff body. Returns (header, body, true) when u
// is one of: edit, hashline_edit, apply_patch, write. Body uses the lilac
// tool-rail so it groups visually under the call card; +/- lines get
// green/red. Returns ("", "", false) for every other tool so the caller
// can fall back to the plain json summary path.
func (r *StreamRenderer) renderEditPreview(u types.ToolUse) (string, string, bool) {
	switch u.Name {
	case "edit":
		return r.previewEdit(u.Input)
	case "hashline_edit":
		return r.previewHashlineEdit(u.Input)
	case "apply_patch":
		return r.previewApplyPatch(u.Input)
	case "write":
		return r.previewWrite(u.Input)
	}
	return "", "", false
}

// previewEdit renders the edit tool: replace Nth `old` with `new`.
//
//	path
//	▎ - <old line 1>
//	▎ + <new line 1>
func (r *StreamRenderer) previewEdit(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	old, _ := in["old"].(string)
	newStr, _ := in["new"].(string)
	if path == "" {
		return "", "", true
	}
	header := r.styles.DiffPath.Render(shortenPath(path))
	if occ, ok := numericField(in["occurrence"]); ok && occ != 1 {
		header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(occ %d)", occ))
	}
	var lines []string
	for _, l := range splitKeepEmpty(old) {
		lines = append(lines, r.styles.DiffDel.Render("- "+l))
	}
	for _, l := range splitKeepEmpty(newStr) {
		lines = append(lines, r.styles.DiffAdd.Render("+ "+l))
	}
	return header, r.diffBody(lines), true
}

// previewWrite renders the write tool. Treats the whole content as added
// lines so the user sees what is being written — capped in compact mode.
func (r *StreamRenderer) previewWrite(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	content, _ := in["content"].(string)
	if path == "" {
		return "", "", true
	}
	header := r.styles.DiffPath.Render(shortenPath(path))
	all := splitKeepEmpty(content)
	header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(write, %d lines)", len(all)))
	lines := make([]string, 0, len(all))
	for _, l := range all {
		lines = append(lines, r.styles.DiffAdd.Render("+ "+l))
	}
	return header, r.diffBody(lines), true
}

// previewApplyPatch parses the unified diff in `patch` and renders each
// hunk colored. File headers (`diff --git`, `---`, `+++`, `@@`) stay
// dimmed-meta; +/- lines pick up the green/red diff styles.
func (r *StreamRenderer) previewApplyPatch(in map[string]any) (string, string, bool) {
	patch, _ := in["patch"].(string)
	if patch == "" {
		return "", "", true
	}
	files := patchFiles(patch)
	header := r.styles.DiffPath.Render(strings.Join(files, ", "))
	if len(files) == 0 {
		header = r.styles.DiffMeta.Render("(empty patch)")
	}
	var lines []string
	for _, raw := range splitKeepEmpty(strings.TrimRight(patch, "\n")) {
		lines = append(lines, r.colorPatchLine(raw))
	}
	return header, r.diffBody(lines), true
}

// previewHashlineEdit renders each anchored edit op as a styled block.
//
//	path  (3 edits)
//	▎ 42#VK replace
//	▎ + new line
func (r *StreamRenderer) previewHashlineEdit(in map[string]any) (string, string, bool) {
	path, _ := in["path"].(string)
	edits, _ := in["edits"].([]any)
	if path == "" {
		return "", "", true
	}
	header := r.styles.DiffPath.Render(shortenPath(path))
	if dry, _ := in["dry_run"].(bool); dry {
		header += "  " + r.styles.DiffMeta.Render("(dry-run)")
	}
	header += "  " + r.styles.DiffMeta.Render(fmt.Sprintf("(%d edit%s)", len(edits), pluralS(len(edits))))
	var lines []string
	for _, e := range edits {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		pos, _ := m["pos"].(string)
		op, _ := m["op"].(string)
		lines = append(lines, r.styles.DiffMeta.Render(fmt.Sprintf("%s %s", pos, op)))
		raw, _ := m["lines"].([]any)
		for _, l := range raw {
			s, _ := l.(string)
			lines = append(lines, r.styles.DiffAdd.Render("+ "+s))
		}
	}
	return header, r.diffBody(lines), true
}

// colorPatchLine picks the style for one raw unified-diff line.
func (r *StreamRenderer) colorPatchLine(l string) string {
	switch {
	case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"),
		strings.HasPrefix(l, "diff "), strings.HasPrefix(l, "@@"),
		strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file"),
		strings.HasPrefix(l, "deleted file"), strings.HasPrefix(l, "rename "),
		strings.HasPrefix(l, "similarity "):
		return r.styles.DiffMeta.Render(l)
	case strings.HasPrefix(l, "+"):
		return r.styles.DiffAdd.Render(l)
	case strings.HasPrefix(l, "-"):
		return r.styles.DiffDel.Render(l)
	default:
		return r.styles.ToolPrev.Render(l)
	}
}

// diffBody wraps a slice of already-styled lines in the lilac tool rail
// (same as renderToolResult) and applies the compact-mode line cap. When
// every line would render empty the empty string is returned so the
// caller can omit the body entirely.
func (r *StreamRenderer) diffBody(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	hidden := 0
	if !r.verbose && len(lines) > diffPreviewLinesCompact {
		hidden = len(lines) - diffPreviewLinesCompact
		lines = lines[:diffPreviewLinesCompact]
	}
	rail := r.styles.ToolRail.Render("▎")
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = rail + " " + ln
	}
	body := strings.Join(out, "\n")
	if hidden > 0 {
		body += "\n" + rail + " " + r.styles.Dim.Render(fmt.Sprintf("… +%d more", hidden))
	}
	return body
}

// patchFiles returns the list of `+++ b/<path>` (or `--- a/<path>`)
// targets from a unified diff. Used only for the header label; ordering
// preserves the patch order.
func patchFiles(patch string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.Split(patch, "\n") {
		var p string
		switch {
		case strings.HasPrefix(l, "+++ b/"):
			p = strings.TrimPrefix(l, "+++ b/")
		case strings.HasPrefix(l, "+++ "):
			p = strings.TrimPrefix(l, "+++ ")
		case strings.HasPrefix(l, "--- b/") && len(out) == 0:
			p = strings.TrimPrefix(l, "--- b/")
		}
		if p == "" || p == "/dev/null" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// splitKeepEmpty splits on \n preserving trailing empty entries. Avoids the
// strings.Split surprise where "" returns [""].
func splitKeepEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// numericField coerces a JSON number (float64 from encoding/json) or int into int.
func numericField(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	}
	return 0, false
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// renderToolResult renders a tight preview of the output. Lilac left rail
// (▎) prefixes each preview line so the block visually groups under the
// tool call card. Errors get colored. Tool stdout is ANSI-stripped before
// display — raw escape sequences (cursor moves, clear-line) from subprocesses
// like `go test` would otherwise blit over the TUI chrome inside altscreen.
// Blank lines are dropped so the preview stays compact even when the
// underlying output is double-spaced. Overflow is signalled inline as
// `… +N more` on the last preview line rather than spending a row on a
// truncation footer.
func (r *StreamRenderer) renderToolResult(res types.ToolResult) string {
	clean := ansi.Strip(res.Content)
	totalBytes := len(clean)
	lines := compactLines(clean)
	if len(lines) == 0 {
		return ""
	}
	// refusal/denial: tool ran the safety/approval path and was blocked
	// rather than failing. Marker prefixes come from shell.go ("refused by
	// user:"), safety/shell.go + safety/paths.go ("refused: …"), and write
	// filters ("path … denied by write filter"). These read as "blocked,
	// not broken" — use the yellow warn palette so the eye distinguishes
	// them from real bash errors at a glance.
	isRefusal := res.IsError && len(lines) > 0 && (strings.HasPrefix(lines[0], "refused") || strings.Contains(lines[0], "denied by"))

	style := r.styles.ToolPrev
	switch {
	case isRefusal:
		style = r.styles.Warn
	case res.IsError:
		style = r.styles.Error
	}

	// bash error/refusal preview: replace the leading "exit N" or
	// "refused …" line with the originating command rendered on a colored
	// bg badge. Reads "this cmd failed/was blocked" at a glance instead of
	// forcing the user to scroll up and correlate the bare exit/refusal with
	// the originating tool call.
	var headerOverride string
	if res.IsError && len(lines) > 0 && strings.HasPrefix(lines[0], "exit ") {
		if use, ok := r.toolUses[res.UseID]; ok && use.Name == "bash" {
			if cmd, _ := use.Input["command"].(string); cmd != "" {
				exitTag := lines[0]
				lines = lines[1:]
				cmdRendered := r.styles.ErrorCmd.Render("$ " + shortenPathsInline(cmd))
				headerOverride = cmdRendered + " " + r.styles.Dim.Render("("+exitTag+")")
			}
		}
	} else if isRefusal {
		if use, ok := r.toolUses[res.UseID]; ok && use.Name == "bash" {
			if cmd, _ := use.Input["command"].(string); cmd != "" {
				cmdRendered := r.styles.WarnCmd.Render("$ " + shortenPathsInline(cmd))
				headerOverride = cmdRendered
			}
		}
	}

	hidden := 0
	cap := r.previewLines()
	if cap >= 0 && len(lines) > cap {
		hidden = len(lines) - cap
		lines = lines[:cap]
	}
	rail := r.styles.ToolRail.Render("▎")
	rendered := make([]string, 0, len(lines)+1)
	if headerOverride != "" {
		rendered = append(rendered, rail+" "+headerOverride)
	}
	for _, ln := range lines {
		rendered = append(rendered, rail+" "+style.Render(shortenPathsInline(ln)))
	}
	out := strings.Join(rendered, "\n")
	switch {
	case hidden > 0:
		// surface both hidden-row count *and* total payload size — `+1 more`
		// alone hid the fact the next click might dump 50 KB.
		out += r.styles.Dim.Render(fmt.Sprintf(" … +%d more · %s", hidden, humanBytes(totalBytes)))
	case totalBytes >= 1024:
		// fully shown but heavy — surface size so user knows the LLM is
		// paying for a chunky payload even if scrollback hides it.
		out += r.styles.Dim.Render(fmt.Sprintf(" · %s", humanBytes(totalBytes)))
	}
	return out + "\n"
}

// renderThinking styles a chain-of-thought block: every line dimmed +
// italic and prefixed with a quiet `·` glyph so it visually recedes behind
// the actual answer. ANSI strip + control sanitize follow the same rules
// as tool output so escapes can't blit over chrome.
func (r *StreamRenderer) renderThinking(s string) string {
	if !r.showThoughts {
		return ""
	}
	clean := ansi.Strip(strings.TrimRight(s, "\n"))
	if clean == "" {
		return ""
	}
	lines := strings.Split(clean, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = sanitizeControl(ln)
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, r.styles.Dim.Render("·")+" "+r.styles.Thought.Render(ln))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// compactLines splits clean output and drops blank/whitespace-only lines so
// previews don't waste rows on padding. Order is preserved. Non-printable
// control bytes (NUL, BS, BEL, raw CR — anything below 0x20 except tab) are
// replaced with `·` per line: grep accidentally hitting a binary blob,
// curl spitting raw bytes, etc. would otherwise blit blank rows + scrambled
// glyphs into the terminal even after ansi.Strip.
func compactLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = sanitizeControl(l)
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return out
}

// sanitizeControl drops every byte < 0x20 (except tab) and 0x7f. ansi.Strip
// handles CSI/OSC sequences; this catches the bare control bytes (NUL, CR,
// BS, BEL, ...) that wreck terminal layout when raw bytes leak in. Dropping
// (not substituting) lets a line of pure NULs collapse to empty so compactLines
// then discards it — no rows wasted on what was binary garbage.
func sanitizeControl(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || (r >= 0x20 && r != 0x7f) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// summarizeArgs renders the tool input map as a single-line, truncated
// string. When the input has a single primary key (path/pattern/cmd/name/
// query/file_path/regex) the JSON wrapping is dropped and just the value is
// shown — `pattern: foo` rather than `{"pattern":"foo"}`. Falls back to
// compact JSON for everything else.
func summarizeArgs(in map[string]any, budget int) string {
	if len(in) == 0 {
		return ""
	}
	if s, ok := summarizeSingleKey(in); ok {
		return truncateRunes(s, budget)
	}
	b, err := json.Marshal(shortenPathishValues(in))
	if err != nil {
		return "{...}"
	}
	return truncateRunes(string(b), budget)
}

// shortenPathishValues returns a shallow copy of in with values for path-ish
// keys collapsed to cwd-relative / ~-prefixed form. Used in the multi-key
// summary path so grep-style `{glob, path}` calls don't blow tool-card width
// with /Users/<name>/projects/<repo>/... repeated on every render.
func shortenPathishValues(in map[string]any) map[string]any {
	pathish := map[string]bool{"path": true, "file_path": true}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if pathish[k] {
			if s, ok := v.(string); ok {
				out[k] = shortenPath(s)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// summarizeSingleKey returns a `key: value` summary when in has exactly one
// entry whose key is one of the well-known primaries. Strings render
// unquoted; other scalars use json so booleans/numbers stay legible.
// Path-shaped keys get cwd/home prefix stripping so tool cards don't waste
// width on `/Users/<name>/projects/<repo>/` repeated on every read call.
func summarizeSingleKey(in map[string]any) (string, bool) {
	if len(in) != 1 {
		return "", false
	}
	primary := map[string]bool{
		"path": true, "pattern": true, "cmd": true, "command": true,
		"name": true, "query": true, "file_path": true, "regex": true,
	}
	pathish := map[string]bool{"path": true, "file_path": true}
	// pathBearing keys hold free-form text that may *embed* absolute paths
	// (e.g. `cmd: cd /Users/.../bee && go test`). Inline-shorten those so
	// the tool card doesn't waste 30 chars on the home prefix every render.
	pathBearing := map[string]bool{"cmd": true, "command": true}
	for k, v := range in {
		if !primary[k] {
			return "", false
		}
		switch x := v.(type) {
		case string:
			val := x
			switch {
			case pathish[k]:
				val = shortenPath(val)
			case pathBearing[k]:
				val = shortenPathsInline(val)
			}
			return k + ": " + val, true
		default:
			b, err := json.Marshal(x)
			if err != nil {
				return "", false
			}
			return k + ": " + string(b), true
		}
	}
	return "", false
}

// pathRoots caches cwd + home once so summarizeArgs stays allocation-light
// across the bursty tool-call render path. getwd/UserHomeDir hit syscalls
// on every call otherwise.
var (
	pathRootsOnce sync.Once
	cachedCwd     string
	cachedHome    string
)

func initPathRoots() {
	if d, err := os.Getwd(); err == nil {
		cachedCwd = filepath.Clean(d)
	}
	if h, err := os.UserHomeDir(); err == nil {
		cachedHome = filepath.Clean(h)
	}
}

// shortenPathsInline rewrites every absolute path token embedded inside s to
// its cwd-relative form (or `~/...` when only home matches). Used for free-
// form strings that contain paths — bash `cmd:` summaries (`cd /Users/x/
// projects/bee && go test` → `cd . && go test`) and raw tool stdout lines.
// Pure prefix-substring replace, cheap on the hot render path. Empty cwd /
// home (e.g. tests, root user) degrades to a no-op.
func shortenPathsInline(s string) string {
	if s == "" {
		return s
	}
	pathRootsOnce.Do(initPathRoots)
	if cachedCwd != "" {
		s = strings.ReplaceAll(s, cachedCwd+string(filepath.Separator), "")
		s = strings.ReplaceAll(s, cachedCwd, ".")
	}
	if cachedHome != "" {
		s = strings.ReplaceAll(s, cachedHome+string(filepath.Separator), "~"+string(filepath.Separator))
		s = strings.ReplaceAll(s, cachedHome, "~")
	}
	return s
}

// humanBytes formats n as a short decimal-prefixed size ("812 B", "2.4 KB",
// "1.1 MB"). Decimal (1000), not binary — reads like `ls -h` and aligns
// with token-cost intuition more than disk-block intuition.
func humanBytes(n int) string {
	const (
		kb = 1000
		mb = 1000 * 1000
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	}
}

// shortenPath collapses absolute paths under cwd → relative; otherwise
// under home → `~/...`. Non-absolute or out-of-tree paths pass through.
func shortenPath(p string) string {
	if p == "" || !filepath.IsAbs(p) {
		return p
	}
	pathRootsOnce.Do(initPathRoots)
	cleaned := filepath.Clean(p)
	if cachedCwd != "" {
		if rel, err := filepath.Rel(cachedCwd, cleaned); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "./"
			}
			return rel
		}
	}
	if cachedHome != "" && strings.HasPrefix(cleaned, cachedHome+string(filepath.Separator)) {
		return "~" + cleaned[len(cachedHome):]
	}
	return p
}

// truncateRunes caps a display string at n runes, suffixing an ellipsis when
// it had to cut. Operates on runes so multibyte content (paths with unicode)
// doesn't get sliced mid-codepoint.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}
