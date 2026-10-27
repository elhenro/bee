package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textarea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/checkpoint"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// State is the high-level TUI mode. Most input is gated on it.
type State int

const (
	StateIdle State = iota
	StateStreaming
	StateError
)

// Model is the bubbletea root model for bee's interactive REPL.
type Model struct {
	// chrome
	styles *Styles // shared by pointer: Model is copied by value on every Update/View call
	keys   KeyMap
	width  int
	height int

	// chat state
	state    State
	messages []types.Message
	stream   *StreamRenderer
	partial  string // live-streaming buffer
	lastErr  string

	// progressiveStream enables the head-flush path. On by default; disable
	// with BEE_STREAM_PROGRESSIVE=0 to fall back to pure tail-clipping.
	progressiveStream bool

	// input — multi-line textarea so shift+enter / ctrl+j insert newlines
	// while enter still submits via handleKey before textarea sees it.
	input textarea.Model

	// seedPrompt auto-submits one turn on startup. Set by skill dispatch
	// (`bee research <topic>`) when stdout is a tty: the dispatcher seeds
	// "/<skill> <args>" so the existing slash-skill path runs it live and
	// the user can watch tool cards + thoughts, then steer. Fired once
	// from Init via autoSubmitMsg; piped/non-tty dispatch stays headless.
	seedPrompt string

	// suspendedJob tracks a Ctrl-Z'd external command (vim, etc.) so ctrl+o
	// resumes it instead of opening a new one. nil when none is parked.
	suspendedJob *suspendedJob

	// status bar metadata
	cwd      string
	model    string
	scope    string
	caveLvl  caveman.Level
	thinking string // internal mirror of the role's baked reasoning budget
	role     string // worker | scout | queen (shift+tab cycles)
	// yolo auto-approves dangerous shell on any role (alt+y toggles it).
	// Mirrors eng.Cfg.Yolo; rendered as a loud red badge while on.
	yolo bool
	// glowFrame drives the rainbow input animation while the queen role is
	// active. Incremented by glowTickMsg on its own self-rearming tick
	// (independent of the streaming loaderFrame so the glow lives at idle too).
	glowFrame int
	showHelp  bool // toggle the bottom hint line (`?` to show)

	// engine + plumbing — nil in tests
	eng       *loop.Engine
	ctx       context.Context
	cancelRun context.CancelFunc

	// approval modal
	approval ApprovalModel
	// approver is the channel adapter the shell tool talks to. nil disables
	// the dangerous-command prompt flow (legacy behavior).
	approver *Approver

	// ask_user picker modal + the adapter the ask_user tool blocks on. nil
	// asker means the tool auto-resolves to the recommended option.
	askModel AskModel
	asker    *Asker

	// escalate picker — shown after the escalate tool bails with options.
	// Renders inline under the live region; picking submits the chosen
	// option as the next user turn.
	escalate EscalateModel

	// planmode picker — shown after a clean plan-mode turn so the user can
	// switch into a build mode (and optionally a fresh session) instead of
	// silently no-opping in plan mode. pendingPlan holds the plan text
	// captured when it opens, used to seed a fresh session.
	planmode    PlanModeModel
	pendingPlan string

	// slash command registry + palette
	cmds          *commands.Registry
	skills        SkillsLister
	palette       PaletteModel
	quitRequested bool

	// session tree modal — toggled by Alt+T or /tree.
	tree          *SessionTree
	treeRequested bool

	// resume picker modal — opened by /resume.
	resume          *ResumePicker
	resumeRequested bool

	// rewind picker modal — opened by /rewind. Lists prior turns; picking
	// restores code and/or conversation to that checkpoint.
	rewind          *RewindPicker
	rewindRequested bool
	// checkpoints snapshots the work-tree per turn into a hidden shadow repo
	// so rewind can restore code. nil disables the feature gracefully.
	checkpoints *checkpoint.Store

	// login modal — opened by /login (no args).
	loginPane      *LoginPane
	loginRequested bool

	// picker modal — opened by Ctrl+P or /model (no args). Lists providers
	// + their models; commit publishes PickedMsg to switch + persist.
	picker          *Picker
	pickerRequested bool

	// role pane — opened by /role (no args). Arrow keys pick role;
	// enter commits, esc closes.
	rolePane      *RolePane
	roleRequested bool

	// effort pane — opened by /effort (no args). Pins the reasoning budget,
	// overriding the role-baked default.
	effortPane      *EffortPane
	effortRequested bool

	// agentView is the bgreg-backed pane. Left arrow opens it; tick-driven
	// 1s refresh keeps live bg bees up to date.
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

	// pendingImage holds raw image bytes staged via Ctrl+I; attached to the
	// next user message on submit and cleared after.
	pendingImage []byte
	// pendingImages holds image blocks staged from dragged/pasted file paths
	// (the path is replaced by an "[Image: name]" label in the input as it's
	// pasted). Drained alongside pendingImage on submit.
	pendingImages []types.ContentBlock

	// printedCount tracks how many messages from m.messages have already
	// been emitted to the terminal scrollback via tea.Println. The live
	// View() never renders past messages — they live in native scrollback.
	// flush() emits the slice m.messages[printedCount:] and advances this.
	printedCount int
	// scroll orders everything that goes to scrollback; shared by pointer
	// like styles, so every copy of the Model queues into the same line
	scroll *scrollback

	// verbose unlocks full tool-output rendering. Off by default keeps the
	// transcript compact; toggle with ctrl+v or pass --verbose.
	verbose bool

	// toolOutputExpanded tracks the ctrl+o toggle state. Scrollback is
	// immutable (printed once via tea.Println), so each press re-prints the
	// last tool result at the flipped detail level rather than editing in place.
	toolOutputExpanded bool

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

	// showCallDetail prints the whole call under a labeled tool call card.
	// Toggle via /settings; persists to config.
	showCallDetail bool

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

	// statusLine is the ordered list of status-line item ids (see
	// statusline.go) seeded from cfg.TUI.StatusLine. nil = use the built-in
	// default order; a non-nil empty slice hides the line. Unknown ids are
	// dropped at render time so a damaged config never blocks startup.
	statusLine []string

	// quitArmed is true after a first ctrl+d. Within quitConfirmWindow a
	// second ctrl+d quits; any other key clears the armed state. Ctrl+c is
	// not gated by this — POSIX cancel convention is single-press.
	quitArmed   bool
	quitArmedAt time.Time

	// outerPadLR inserts N cells of left+right padding around the live TUI
	// region (top bar, streaming partial, input, modals). 0 = off. Capped at 8
	// on write so a tiny terminal never collapses to a sliver. Mirrors
	// eng.Cfg.OuterPadLR; persisted to ~/.bee/config.toml as
	// `outer_pad_lr` via /settings. Finalized messages flushed to scrollback
	// are NOT padded — the wrap is applied at the View() boundary.
	outerPadLR int

	// updatePrompt is the four-button modal surfaced when the hourly checker
	// finds that main has new commits. Inactive until updateAvailableMsg fires.
	updatePrompt UpdatePrompt

	// state groups, embedded so fields stay m.<name>: see model_*.go
	turnState
	flushState
	promptCycle
	handoffFlow
	compactFlow
	engineFeeds
	loaderState
	watchdogState
	costState
	introState
	updateState
	tutorialFlow
	goalLoop
}
