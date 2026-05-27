package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textarea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// State is the high-level TUI mode. Most input is gated on it.
type State int

const (
	StateIdle State = iota
	StateStreaming
	StateAwaitingApproval
	StateError
)

// Model is the bubbletea root model for bee's interactive REPL.
type Model struct {
	// chrome
	styles Styles
	keys   KeyMap
	width  int
	height int

	// chat state
	state    State
	messages []types.Message
	stream   *StreamRenderer
	partial  string // live-streaming buffer
	lastErr  string

	// streamFlushed holds the prefix of m.partial that has already been
	// pushed into terminal scrollback via tea.Println by the progressive
	// flush path. View() renders only m.partial[len(streamFlushed):]; flush()
	// strips the prefix from the matching final assistant message so the
	// settled head doesn't get re-printed underneath.
	streamFlushed string
	// streamFenceOpen tracks whether the already-flushed prefix sits inside
	// an unclosed ``` fence. Drives the glamour-vs-raw rendering choice for
	// the next chunk: code-block content gets shipped raw (preserving its
	// monospace layout) while prose chunks flow through glamour for full
	// markdown styling (headings, lists, bold, links).
	streamFenceOpen bool
	// pendingFlushedPrefix carries streamFlushed across the partial reset
	// (turnDoneMsg / liveMsgMsg) so the next flush() call can suppress the
	// already-printed prefix. Consumed (cleared) on first flush() call after
	// commit, even if no matching message was found.
	pendingFlushedPrefix string
	// progressiveStream enables the head-flush path. On by default; disable
	// with BEE_STREAM_PROGRESSIVE=0 to fall back to pure tail-clipping.
	progressiveStream bool

	// input — multi-line textarea so shift+enter / ctrl+j insert newlines
	// while enter still submits via handleKey before textarea sees it.
	input textarea.Model

	// status bar metadata
	cwd      string
	model    string
	scope    string
	caveLvl  caveman.Level
	thinking string // off | low | medium | high
	mode     string // plan | auto | edit (shift+tab cycles)
	showHelp bool   // toggle the bottom hint line (`?` to show)

	// engine + plumbing — nil in tests
	eng       *loop.Engine
	ctx       context.Context
	cancelRun context.CancelFunc

	// queue holds follow-up messages submitted via Alt+Enter; drained
	// one-at-a-time when each turn finishes.
	queue []string

	// approval modal
	approval ApprovalModel
	// approver is the channel adapter the shell tool talks to. nil disables
	// the dangerous-command prompt flow (legacy behavior).
	approver *Approver

	// slash command registry + palette
	cmds          *commands.Registry
	skills        SkillsLister
	palette       PaletteModel
	quitRequested bool

	// session tree modal — toggled by Ctrl+T or /tree.
	tree           *SessionTree
	treeRequested  bool

	// resume picker modal — opened by /resume.
	resume          *ResumePicker
	resumeRequested bool

	// login modal — opened by /login (no args).
	loginPane      *LoginPane
	loginRequested bool

	// picker modal — opened by Ctrl+P or /model (no args). Lists providers
	// + their models; commit publishes PickedMsg to switch + persist.
	picker          *Picker
	pickerRequested bool

	// effort pane — opened by /effort (no args). Arrow keys pick level;
	// enter commits, esc closes.
	effortPane      *EffortPane
	effortRequested bool

	// hive pane — opened by Left arrow on an empty input. Right arrow
	// returns to the chat view. Mirrors Ctrl+H toggle path.
	hive *Hive

	// agentView is the bgreg-backed pane that supersedes hive.RenderFull.
	// Left arrow opens it; tick-driven 1s refresh keeps live bg bees up to
	// date. Hive stays for the bottom-bar hex strip widget.
	agentView *AgentView

	// settings pane — opened by /settings. Toggles verbose tool output and
	// agent-thought visibility; each toggle persists to ~/.bee/config.toml.
	settingsPane      *SettingsPane
	settingsRequested bool

	// tools pane — opened by /tools. Toggle enable/disable per tool; persists
	// to disabled_tools in ~/.bee/config.toml and applies on the next turn.
	toolsPane      *ToolsPane
	toolsRequested bool

	// @-triggered file picker
	atpicker AtPickerModel

	// ctrl+r reverse history search picker
	history HistoryPickerModel

	// up/down prompt cycling. cycleActive is true mid-cycle; cycleEntries is
	// the deduped newest-first list loaded once per cycle session (includes
	// prior sessions via ~/.bee/history); cycleIdx is -1 before the first
	// up press; cycleStash holds the buffer the user had typed before
	// cycling started so down past the newest restores it.
	cycleActive  bool
	cycleEntries []string
	cycleStash   string
	cycleIdx     int

	// pendingImage holds raw image bytes staged via Ctrl+I; attached to the
	// next user message on submit and cleared after.
	pendingImage []byte

	// streamCh receives text deltas from the engine via Engine.StreamCh.
	// nil in tests; lifetime owned by the caller of WithStreamCh.
	streamCh chan string

	// thinkCh receives chain-of-thought deltas from the engine via
	// Engine.ThinkCh so reasoning renders live above the answer instead
	// of dumping all at once when the stream ends.
	thinkCh chan string
	// thinkPartial is the accumulated reasoning buffer for the in-flight
	// turn. Rendered dim+italic above m.partial while streaming. Cleared
	// when the final BlockThinking lands via liveMsgMsg/turnDoneMsg.
	thinkPartial string

	// liveMsgCh receives each persisted assistant/tool message from the
	// engine's LiveMsgCh so cards render mid-Run instead of only on done.
	liveMsgCh chan types.Message

	// warnCh receives transient operational notices from Engine.WarnCh —
	// stream retries, watchdog hiccups, etc. The latest one shows as a dim
	// line, then fades after warningTTL.
	warnCh chan string
	// warning is the currently-displayed notice ("" when none active).
	warning string
	// warningExpires is the wall-clock instant after which the warning fades.
	warningExpires time.Time

	// loaderFrame drives the pre-token streaming animation. Incremented by
	// loaderTickMsg while state == StateStreaming and partial is empty.
	loaderFrame int

	// turnStartedAt is wall-clock when the current turn left submit(). Zero
	// when no turn in flight. Top-bar timer reads time.Since on every tick
	// so the elapsed string updates live without per-second state.
	turnStartedAt time.Time
	// lastTurnDuration is how long the most recent turn took end-to-end.
	// Set on every turnDoneMsg path (success, cancel, error). Persists in
	// the top bar after streaming ends until the next submit clears it.
	lastTurnDuration time.Duration

	// compacting is true while an async /compact goroutine is running.
	// renderLive uses this to keep the loader animation alive while the
	// summarization LLM call streams, since state stays StateIdle.
	compacting bool

	// queuedMidCompact holds plain-text submits typed while compacting.
	// Drained by onCompactDone — the queued text is fed back through
	// submit() once the compacted history is in place. Slash commands and
	// shell-bangs are not queued (they're meta, weird to delay). Last
	// queued wins on rapid retyping.
	queuedMidCompact string

	// printedCount tracks how many messages from m.messages have already
	// been emitted to the terminal scrollback via tea.Println. The live
	// View() never renders past messages — they live in native scrollback.
	// flush() emits the slice m.messages[printedCount:] and advances this.
	printedCount int

	// costs is the per-session usage/dollar accumulator shared with the
	// engine. Read for the top-bar total and the /cost monitor pane.
	costs *cost.Tracker
	// costPane is the Ctrl+Y modal — opens on demand, claims keys while open.
	costPane *CostPane
	costRequested bool
	// costFlashFrame counts up while a brief post-turn animation plays in
	// the top bar: colour-cycling the dollar amount and showing the delta.
	// costFlashUntil holds the inclusive end frame (0 = no flash active).
	// costFlashDelta is the USD added by the most recent turn.
	// costPrevCalls tracks how many events were already in the tracker the
	// last time we looked — used to detect "a new turn just landed".
	costFlashFrame int
	costFlashUntil int
	costFlashDelta float64
	costPrevCalls  int

	// verbose unlocks full tool-output rendering. Off by default keeps the
	// transcript compact; toggle with ctrl+v or pass --verbose.
	verbose bool

	// showThoughts gates BlockThinking rendering in scrollback. Default true.
	// Toggle via /settings; persists to ~/.bee/config.toml.
	showThoughts bool

	// showNudges gates render of synthetic `[nudge]` recovery turns from
	// the loop. Default false. Toggle via /settings; persists to config.
	showNudges bool

	// showRecap controls whether a one-line post-turn recap is generated
	// by a side-LLM call after each successful turn. Default false (extra
	// tokens). Toggle via /settings; persists to config. Disabled = no
	// generation, no render.
	showRecap bool

	// recapGen is bumped on every submit so a pending idle-delay tick for
	// the previous turn drops its scheduled side-call. Only when the gen
	// observed by the firing tick still matches m.recapGen do we generate.
	recapGen int

	// compact strips the spacing layer (gutter, inter-turn blank line,
	// user bg-tint, OSC 133 zones) for a denser layout. Default false =
	// clean mode. Toggle via /settings; persists to config.
	compact bool

	// showContextBar reveals the bottom-edge thin context-fill strip. Default
	// false — the top-bar bee-glyph hex fill already conveys utilisation.
	// Toggle via /settings; persists to config.
	showContextBar bool

	// highlight gates chroma syntax-highlighting on tool output, diffs,
	// file content, and bash command summaries. Default true. Toggle via
	// /settings; persists to config.
	highlight bool

	// shellBangSilent controls the default behavior of `!cmd`. true (default)
	// runs locally without forwarding to the LLM; false legacy-style submits
	// the output as a user turn. `!!cmd` always runs in the opposite mode.
	// Toggle via /settings; persists to config.
	shellBangSilent bool

	// top-bar chrome toggles. Default true preserves the original status row;
	// flipping all five off collapses the entire line. Toggle via /settings;
	// persists to config.
	showBee        bool
	showContextPct bool
	showModel      bool
	showCwd        bool
	showEffort     bool
	// showTurnTimer toggles the top-bar "⏱ 1.4s" chip — live while a turn
	// streams, final after it ends. Default true. Off completely hides the
	// timer (live + final) for users who prefer a quieter top bar.
	showTurnTimer   bool
	showGitBranch   bool
	showTotalTokens bool

	// quitArmed is true after a first ctrl+d. Within quitConfirmWindow a
	// second ctrl+d quits; any other key clears the armed state. Ctrl+c is
	// not gated by this — POSIX cancel convention is single-press.
	quitArmed   bool
	quitArmedAt time.Time

	// intro animation — plays above the input bar without blocking. Frames
	// are built lazily on the first tick once width is known. introDone
	// keeps the space reserved after the last frame so the live region
	// doesn't shrink; renderIntro then draws a static "bee v<x>" placeholder.
	// introDoneFrame drives the post-intro pulse (two bold-flashes on "bee");
	// it ticks up until introPulseFrames then settles.
	introActive    bool
	introDone      bool
	introStyle     IntroStyle
	introFrames    []IntroFrame
	introIdx       int
	introDoneFrame int

	// showBanner mirrors cfg.ShowBanner. Persisted via /settings; takes
	// effect on the NEXT launch (intro plays once at startup).
	showBanner bool

	// showLoader gates the streaming "generating" animation live. Toggle
	// via /settings; persists across launches.
	showLoader bool

	// updatePrompt is the four-button modal surfaced when the hourly checker
	// finds that main has new commits. Inactive until updateAvailableMsg fires.
	updatePrompt UpdatePrompt
	// updateSeenSession suppresses re-prompts for the remainder of this
	// session after the user picks "later". Re-checking still happens — the
	// gate is between probe + show, not between probe + skip.
	updateSeenSession bool
	// updateApplying flags an in-flight install subprocess so the user can't
	// trigger a second one before the first finishes.
	updateApplying bool
}
