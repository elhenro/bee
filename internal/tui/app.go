package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/skills"
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

	// compacting is true while an async /compact goroutine is running.
	// renderLive uses this to keep the loader animation alive while the
	// summarization LLM call streams, since state stays StateIdle.
	compacting bool

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

	// compact strips the pi-spacing layer (gutter, inter-turn blank line,
	// user bg-tint, OSC 133 zones) for the dense pre-pi layout.
	// Default false = clean mode. Toggle via /settings; persists to config.
	compact bool

	// showContextBar reveals the bottom-edge thin context-fill strip. Default
	// false — the top-bar bee-glyph hex fill already conveys utilisation.
	// Toggle via /settings; persists to config.
	showContextBar bool

	// quitArmed is true after a first ctrl+d. Within quitConfirmWindow a
	// second ctrl+d quits; any other key clears the armed state. Ctrl+c is
	// not gated by this — POSIX cancel convention is single-press.
	quitArmed   bool
	quitArmedAt time.Time

	// intro animation — plays above the input bar without blocking. Frames
	// are built lazily on the first tick once width is known.
	introActive bool
	introStyle  IntroStyle
	introFrames []IntroFrame
	introIdx    int
}

// quitConfirmWindow is how long a single ctrl+d arms the quit-confirm flow.
// After this elapses, the next ctrl+d re-arms instead of quitting.
const quitConfirmWindow = 2 * time.Second

// streamDeltaMsg carries a single text delta from the engine's StreamCh
// into the bubbletea Update loop.
type streamDeltaMsg struct{ Delta string }

// liveMsgMsg carries a freshly-persisted message from the engine into the
// TUI so the scrollback updates the moment the loop appends an assistant
// or tool message — instead of waiting for the whole Run to complete.
type liveMsgMsg struct{ Msg types.Message }

// warningMsg carries a transient operational notice from Engine.WarnCh into
// the bubbletea Update loop. The line shows above chrome and fades after
// warningTTL.
type warningMsg struct{ Text string }

// warningFadeMsg fires after warningTTL to clear an active warning.
type warningFadeMsg struct{}

// warningTTL is how long a transient notice stays on screen.
const warningTTL = 5 * time.Second

// loaderTickMsg drives the streaming-loader animation. Self-rearming while
// state == StateStreaming; dies on its own when the turn finishes.
type loaderTickMsg struct{}

// compactDoneMsg is published when an async /compact goroutine finishes.
// nil err means the summarization succeeded.
type compactDoneMsg struct{ err error }

// loaderTickInterval is the frame cadence. 120ms is fast enough that the
// bee-trail bounce looks alive but slow enough to keep the redraw cost
// invisible on a remote terminal.
const loaderTickInterval = 120 * time.Millisecond

func loaderTickCmd() tea.Cmd {
	return tea.Tick(loaderTickInterval, func(time.Time) tea.Msg { return loaderTickMsg{} })
}

// costTickMsg drives the post-turn cost flash animation. Self-rearms while
// costFlashFrame < costFlashUntil; dies on its own once the flash completes.
type costTickMsg struct{}

// costTickInterval paces the post-turn cost fade. Slow enough that the
// badge breathes once instead of strobing.
const costTickInterval = 160 * time.Millisecond

// costFlashDuration is how many frames a single fade plays for. 8 * 160ms ≈
// 1.3s — brief acknowledgement, no nagging shimmer.
const costFlashDuration = 8

func costTickCmd() tea.Cmd {
	return tea.Tick(costTickInterval, func(time.Time) tea.Msg { return costTickMsg{} })
}

// introTickMsg advances the non-blocking startup intro animation. Self-rearms
// while introActive; the animation lives above the input bar so typing is
// available from frame zero.
type introTickMsg struct{}

func introTickCmd() tea.Cmd {
	return tea.Tick(introFrameDelay, func(time.Time) tea.Msg { return introTickMsg{} })
}

// turnDoneMsg is published when the engine finishes a Run.
type turnDoneMsg struct {
	result loop.RunResult
	err    error
}

// sentinel msgs for unwired panes — slice 3B/3C consume them later.
type (
	openWorkspaceMsg struct{}
	openHiveMsg      struct{}
	openProviderMsg  struct{}
	openTreeMsg      struct{}
	openPaletteMsg   struct{}
	openCostMsg      struct{}
	openLoginMsg     struct{}
	openResumeMsg    struct{}
	openEffortMsg    struct{}
	openSettingsMsg  struct{}
)

// NewModel constructs an idle TUI model. eng may be nil for unit tests.
// A built-in slash command registry is created and seeded — callers that
// want a custom set should call WithCommands on the returned Model.
func NewModel(eng *loop.Engine, cwd, modelName, scope string, lvl caveman.Level) Model {
	ti := textarea.New()
	ti.Placeholder = ""
	ti.Prompt = "› "
	ti.ShowLineNumbers = false
	ti.CharLimit = 16384
	ti.SetHeight(1)
	ti.SetWidth(40)
	// kill the default rounded border + cell padding so the textarea sits
	// flush like the old textinput.
	ti.FocusedStyle.Base = lipgloss.NewStyle()
	ti.BlurredStyle.Base = lipgloss.NewStyle()
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ti.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(colorHoney)
	ti.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(colorHoney)
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colorDim)
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(colorDim)
	ti.Focus()
	ti.Cursor.SetMode(cursor.CursorStatic)
	// enter is reserved for submit (handleKey catches it before Update).
	// Newline binds to shift+enter (modern terminals: Ghostty, Kitty,
	// Wezterm, iTerm w/ CSI u) and ctrl+j (universal fallback — terminals
	// that don't distinguish shift+enter still send ctrl+j on C-j).
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)

	styles := DefaultStyles()
	keys := DefaultKeyMap()

	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)

	thinking := string(llm.ThinkingOff)
	if eng != nil && eng.Cfg.Thinking != "" {
		thinking = string(llm.ParseThinking(eng.Cfg.Thinking))
	}

	mode := string(loop.ModeEdit)
	if eng != nil && eng.Cfg.Mode != "" {
		mode = string(loop.ParseMode(eng.Cfg.Mode))
		// resolve auto: local providers skip the classifier, land in edit.
		if mode == "auto" && (eng.Cfg.Profile == "tiny" || config.IsLocalProvider(eng.Cfg.DefaultProvider)) {
			mode = "edit"
		}
	}

	var pk *Picker
	if eng != nil {
		pk = NewPicker(eng.Cfg)
	}

	return Model{
		styles:   styles,
		keys:     keys,
		state:    StateIdle,
		input:    ti,
		cwd:      cwd,
		model:    modelName,
		scope:    scope,
		caveLvl:  lvl,
		thinking: thinking,
		mode:     mode,
		eng:      eng,
		approval: NewApprovalModel(styles, keys),
		stream:   NewStreamRenderer(styles, 80),
		cmds:     reg,
		palette:  NewPalette(reg, nil),
		tree:     NewSessionTree(),
		resume:   NewResumePicker(),
		history:  NewHistoryPicker(),
		picker:   pk,
		effortPane:   NewEffortPane(),
		settingsPane: NewSettingsPane(),
		hive:         NewHive(),
		agentView:    NewAgentView(),
		showThoughts: true,
	}
}

// WithCostTracker wires the engine's cost.Tracker into the model so the
// top bar and the /cost pane can read it.
func (m Model) WithCostTracker(t *cost.Tracker) Model {
	m.costs = t
	return m
}

// WithIntro enables the non-blocking startup animation. Frames are built
// lazily on the first tick once width is known. BEE_NO_INTRO=1 disables.
func (m Model) WithIntro(style IntroStyle) Model {
	if os.Getenv("BEE_NO_INTRO") == "1" {
		return m
	}
	m.introStyle = style
	m.introActive = true
	return m
}

// WithVerbose seeds verbose tool-output rendering. CLI flag/env var path.
func (m Model) WithVerbose(v bool) Model {
	m.verbose = v
	if m.stream != nil {
		m.stream.SetVerbose(v)
	}
	return m
}

// WithShowThoughts seeds chain-of-thought visibility. Config-driven path.
func (m Model) WithShowThoughts(v bool) Model {
	m.showThoughts = v
	if m.stream != nil {
		m.stream.SetShowThoughts(v)
	}
	return m
}

// WithShowNudges seeds nudge-visibility from config. Default false hides
// the loop's [nudge] recovery turns; setting true reveals them.
func (m Model) WithShowNudges(v bool) Model {
	m.showNudges = v
	if m.stream != nil {
		m.stream.SetShowNudges(v)
	}
	return m
}

// WithCompact seeds compact-mode rendering. Env/config-driven path.
func (m Model) WithCompact(v bool) Model {
	m.compact = v
	if m.stream != nil {
		m.stream.SetCompact(v)
	}
	return m
}

// WithShowContextBar seeds context-bar visibility. Config-driven path.
func (m Model) WithShowContextBar(v bool) Model {
	m.showContextBar = v
	return m
}

// maybeStartCostFlash compares the tracker's call count to what we saw last
// turn; on growth, it captures the cost delta and arms the top-bar flash
// animation. Returns the cmd that drives the first tick (nil = no flash).
func (m *Model) maybeStartCostFlash() tea.Cmd {
	if m.costs == nil {
		return nil
	}
	events := m.costs.Events()
	if len(events) <= m.costPrevCalls {
		return nil
	}
	var delta float64
	for _, e := range events[m.costPrevCalls:] {
		delta += e.USD
	}
	m.costPrevCalls = len(events)
	if delta <= 0 {
		// token usage logged but no priced delta (e.g. local Ollama) — skip
		// the flash so we don't draw attention to a $0 line.
		return nil
	}
	m.costFlashDelta = delta
	m.costFlashFrame = 0
	m.costFlashUntil = costFlashDuration
	return costTickCmd()
}

// WithStreamCh wires a text-delta channel from the engine into the TUI.
// The same channel must be set on Engine.StreamCh so deltas flow.
func (m Model) WithStreamCh(ch chan string) Model {
	m.streamCh = ch
	return m
}

// WithLiveMsgCh wires a live-message channel from the engine into the TUI.
// The same channel must be set on Engine.LiveMsgCh so assistant + tool
// messages render as they're persisted, not only at Run completion.
func (m Model) WithLiveMsgCh(ch chan types.Message) Model {
	m.liveMsgCh = ch
	return m
}

// WithWarnCh wires the engine's transient-notice channel into the TUI so
// stream hiccups + retries surface as a fading line in chrome.
func (m Model) WithWarnCh(ch chan string) Model {
	m.warnCh = ch
	return m
}

// WithInitialMessages preloads scrollback. Used by `bee back <id>` to
// restore a prior session into the TUI on launch. The messages get
// flushed to terminal scrollback via tea.Println from Init().
func (m Model) WithInitialMessages(msgs []types.Message) Model {
	if len(msgs) == 0 {
		return m
	}
	m.messages = append(m.messages, msgs...)
	return m
}

// waitStream returns a tea.Cmd that blocks on the next text delta. The
// pump re-arms itself in Update so the channel keeps draining. Returns
// nil (a no-op cmd) when no channel is wired.
func (m Model) waitStream() tea.Cmd {
	if m.streamCh == nil {
		return nil
	}
	ch := m.streamCh
	return func() tea.Msg {
		d, ok := <-ch
		if !ok {
			return nil
		}
		return streamDeltaMsg{Delta: d}
	}
}

// waitLiveMsg blocks on the next mid-Run message from the engine. Same
// re-arming pattern as waitStream.
func (m Model) waitLiveMsg() tea.Cmd {
	if m.liveMsgCh == nil {
		return nil
	}
	ch := m.liveMsgCh
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return liveMsgMsg{Msg: msg}
	}
}

// waitWarn blocks on the next transient notice from Engine.WarnCh. Same
// re-arming pattern as waitStream — Update re-issues the cmd on receipt.
func (m Model) waitWarn() tea.Cmd {
	if m.warnCh == nil {
		return nil
	}
	ch := m.warnCh
	return func() tea.Msg {
		w, ok := <-ch
		if !ok {
			return nil
		}
		return warningMsg{Text: w}
	}
}

// warningFadeCmd fires once after warningTTL to clear the displayed line.
func warningFadeCmd() tea.Cmd {
	return tea.Tick(warningTTL, func(time.Time) tea.Msg { return warningFadeMsg{} })
}

// flush emits tea.Println for every message in m.messages[printedCount:]
// and advances printedCount. The returned Cmd is nil when there's nothing
// new. Inline-mode bubbletea prints those lines above the live region so
// they fall into the terminal's native scrollback — selectable, copyable,
// and unaffected by our redraws.
//
// printedCount can exceed len(m.messages) after a session swap or fork
// that resets the slice; in that case we re-anchor it without emitting.
func (m *Model) flush() tea.Cmd {
	if m.printedCount > len(m.messages) {
		m.printedCount = len(m.messages)
	}
	if m.printedCount == len(m.messages) {
		return nil
	}
	pending := m.messages[m.printedCount:]
	startIdx := m.printedCount
	m.printedCount = len(m.messages)
	cmds := make([]tea.Cmd, 0, len(pending))
	for i, msg := range pending {
		rendered := m.stream.RenderMessage(msg)
		// renderer may return empty for filtered messages (e.g. hidden
		// [nudge] turns); skip those so we don't blit a stray blank row.
		if rendered == "" {
			continue
		}
		// blank-line gap between turns so scrollback breathes. Skip before
		// the very first message of the session (startIdx+i == 0) so we
		// don't push a stray gap above the chat history on cold start.
		if startIdx+i > 0 {
			rendered = "\n" + rendered
		}
		cmds = append(cmds, tea.Println(rendered))
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Sequence(cmds...)
}

// WithCommands swaps in a caller-provided registry. The palette is rebuilt
// against it so Ctrl+K shows the new set. Skills source is preserved.
func (m Model) WithCommands(r *commands.Registry) Model {
	if r == nil {
		return m
	}
	m.cmds = r
	m.palette = NewPalette(r, m.skills)
	return m
}

// WithSkills wires a skills source into the palette so the picker can list
// commands and skills side-by-side. *skills.Registry already satisfies
// SkillsLister, so callers usually pass their loaded registry directly.
func (m Model) WithSkills(sk SkillsLister) Model {
	m.skills = sk
	m.palette = NewPalette(m.cmds, sk)
	return m
}

// compile-time check: skills.Registry satisfies SkillsLister.
var _ SkillsLister = (*skills.Registry)(nil)

// WithKeyMap swaps in a caller-provided keymap. Used to fold in user
// overrides from ~/.bee/keybindings.json without changing NewModel's signature.
// Approval modal is rebuilt because it holds its own copy of the keys.
func (m Model) WithKeyMap(km KeyMap) Model {
	m.keys = km
	m.approval = NewApprovalModel(m.styles, km)
	return m
}

// side returns a fresh commands.Side bound to the current model. We rebuild
// per call because bubbletea passes Model by value — caching a *Model
// pointer would observe a stale copy after the next Update.
func (m *Model) side() commands.Side { return &tuiSide{m: m} }

// inputHeightCap is how tall the textarea can grow before further newlines
// just scroll inside it — keeps the chrome from devouring the transcript.
const inputHeightCap = 6

// inputGrowForMutation bumps the textarea height to inputHeightCap before
// a keystroke or SetValue can soft-wrap the content. Without this, content
// that wraps past the current height makes the textarea's internal
// viewport scroll YOffset down to keep the cursor visible — and once
// YOffset > 0, no path in the textarea API resets it back. Pre-growing
// gives repositionView room and keeps YOffset at 0; syncInputHeight at the
// end of Update then shrinks back to the actual row count for layout.
//
// textarea.Model holds its viewport as a *viewport.Model pointer, so a
// SetHeight inside View's value-copy mutates the same viewport as the
// persistent Model — but the textarea's own height value field stays at
// whatever Update last assigned. That desync is the root cause of the bug:
// a guard like `if Height() < cap` reads the stale value field, returns
// false, and the viewport.Height never grows for the next keystroke. We
// force SetHeight unconditionally so both fields land in lockstep.
func (m *Model) inputGrowForMutation() {
	m.input.SetHeight(inputHeightCap)
}

// syncInputHeight matches the textarea's render height to its content,
// clamped to [1, inputHeightCap]. Call after any value/cursor mutation.
//
// textarea.LineCount() counts logical lines (split on \n) — it does NOT
// account for soft-wrapped rows when a single long line exceeds the inner
// width. Without counting wrapped rows, typing past one visible row makes
// the viewport scroll inside a 1-row window and earlier text vanishes.
// Compute the visual row count by ceil(width / innerWidth) per logical line.
func (m *Model) syncInputHeight() {
	inner := m.input.Width()
	if inner < 1 {
		inner = 1
	}
	n := 0
	for _, line := range strings.Split(m.input.Value(), "\n") {
		w := uniseg.StringWidth(line)
		rows := 1
		if w > inner {
			rows = (w + inner - 1) / inner
		}
		n += rows
	}
	if n < 1 {
		n = 1
	}
	if n > inputHeightCap {
		n = inputHeightCap
	}
	if m.input.Height() != n {
		m.input.SetHeight(n)
	}
}

// Init satisfies tea.Model. Returns the blink cmd so the cursor pulses,
// plus the stream pump when a delta channel is wired and a flush of any
// resumed-session messages so they land in native scrollback at startup.
func (m Model) Init() tea.Cmd {
	// hide terminal cursor so the rendered textinput cursor is the only one
	// visible. textinput.Blink is deliberately omitted so the cursor is static.
	cmds := []tea.Cmd{tea.HideCursor}
	if c := m.waitStream(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitLiveMsg(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitWarn(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.flush(); c != nil {
		cmds = append(cmds, c)
	}
	if m.introActive {
		cmds = append(cmds, introTickCmd())
	}
	return tea.Batch(cmds...)
}

// Update is the bubbletea main switch.
func (m Model) Update(msg tea.Msg) (resultModel tea.Model, resultCmd tea.Cmd) {
	// pre-grow textarea so any input mutation in this turn (handleKey,
	// SetValue from a palette/picker, etc.) wraps inside a tall viewport
	// instead of scrolling YOffset down and hiding row 0. The defer below
	// shrinks back to the actual row count after the message is processed
	// so the persistent model carries the layout-accurate height.
	m.inputGrowForMutation()
	// shrink textarea back to actual row count once the message is processed
	// so the persistent model carries the layout-accurate height. Runs even
	// on the early returns below (quit gates, pane claims).
	defer func() {
		if mm, ok := resultModel.(Model); ok {
			mm.syncInputHeight()
			resultModel = mm
		}
	}()
	// global hard-quit gate — runs above every pane/modal so the user is
	// never trapped. ctrl+c quits immediately (POSIX cancel convention);
	// ctrl+d requires two presses within quitConfirmWindow. Any other key
	// disarms the confirm so a stray ctrl+d in the input bar doesn't leave
	// the program one-keystroke from death.
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "ctrl+c":
			if m.cancelRun != nil {
				m.cancelRun()
			}
			return m, tea.Quit
		case "ctrl+d":
			if m.quitArmed && time.Since(m.quitArmedAt) <= quitConfirmWindow {
				if m.cancelRun != nil {
					m.cancelRun()
				}
				return m, tea.Quit
			}
			m.quitArmed = true
			m.quitArmedAt = time.Now()
			return m, nil
		default:
			if m.quitArmed {
				m.quitArmed = false
			}
		}
	}
	// modal first: it consumes keys when active.
	if m.approval.Active {
		newApp, cmd := m.approval.Update(msg)
		m.approval = newApp
		if _, ok := msg.(ApprovalDecisionMsg); ok {
			m.state = StateIdle
		}
		return m, cmd
	}

	// session tree modal claims keys while open.
	if m.tree != nil && m.tree.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newT, cmd := m.tree.Update(msg)
			m.tree = newT
			return m, cmd
		}
	}

	// resume picker modal claims keys while open.
	if m.resume != nil && m.resume.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newR, cmd := m.resume.Update(msg)
			m.resume = newR
			return m, cmd
		}
	}

	// cost pane claims keys while open.
	if m.costPane != nil && m.costPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newC, cmd := m.costPane.Update(msg)
			m.costPane = newC
			return m, cmd
		}
	}

	// model picker claims all input while active. modelsLoadedMsg /
	// spinner.TickMsg are routed unconditionally below so async loads still
	// settle into the cache.
	if m.picker != nil && m.picker.Active() {
		switch msg.(type) {
		case tea.KeyMsg, modelsLoadedMsg, spinner.TickMsg:
			newP, cmd := m.picker.Update(msg)
			m.picker = newP
			return m, cmd
		}
	}

	// login pane claims keys while open. async loginActionDoneMsg is
	// routed unconditionally below so the pane can clear its busy flag.
	if m.loginPane != nil && m.loginPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newL, cmd := m.loginPane.Update(msg)
			m.loginPane = newL
			return m, cmd
		}
	}

	// effort pane claims keys while open.
	if m.effortPane != nil && m.effortPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newE, cmd := m.effortPane.Update(msg)
			m.effortPane = newE
			return m, cmd
		}
	}

	// settings pane claims keys while open.
	if m.settingsPane != nil && m.settingsPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newS, cmd := m.settingsPane.Update(msg)
			m.settingsPane = newS
			return m, cmd
		}
	}

	// palette is active: nav keys go to palette, everything else falls
	// through to the main input so the user sees what they type. Filter
	// syncs from input value after handleKey runs (see KeyMsg branch).
	if m.palette.Active {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "up", "down", "enter", "esc", "ctrl+n", "ctrl+p", "ctrl+c":
				newP, cmd := m.palette.Update(msg)
				m.palette = newP
				return m, cmd
			}
		}
	}

	// @-picker claims keys while open.
	if m.atpicker.Active {
		if _, ok := msg.(tea.KeyMsg); ok {
			np, cmd := m.atpicker.Update(msg)
			m.atpicker = np
			return m, cmd
		}
	}

	// history picker claims keys while open.
	if m.history.Active {
		if _, ok := msg.(tea.KeyMsg); ok {
			nh, cmd := m.history.Update(msg)
			m.history = nh
			return m, cmd
		}
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// re-size text input to fit
		m.input.SetWidth(max(0, msg.Width-4))
		m.stream = NewStreamRenderer(m.styles, max(40, msg.Width-2))
		m.stream.SetVerbose(m.verbose)
		m.stream.SetShowThoughts(m.showThoughts)
		m.stream.SetShowNudges(m.showNudges)
		m.stream.SetCompact(m.compact)
		m.palette.SetWidth(msg.Width)
		m.atpicker.SetWidth(msg.Width)
		if m.picker != nil {
			m.picker.SetSize(msg.Width-4, msg.Height-4)
		}
		return m, nil

	case tea.KeyMsg:
		nm, cmd := m.handleKey(msg)
		// palette filter mirrors the main input. Close palette if the user
		// backspaced past the leading "/".
		if mm, ok := nm.(Model); ok && mm.palette.Active {
			val := mm.input.Value()
			if strings.HasPrefix(val, "/") {
				mm.palette.SetFilter(val[1:])
			} else {
				mm.palette.Active = false
			}
			return mm, cmd
		}
		return nm, cmd

	case streamDeltaMsg:
		// append to live partial. View() picks it up next render. The pump
		// re-arms itself so subsequent deltas keep draining.
		m.partial += msg.Delta
		return m, m.waitStream()

	case liveMsgMsg:
		// engine persisted a new assistant/tool message mid-Run; print it to
		// native scrollback right away so the user sees tool cards as they
		// happen instead of only at turnDoneMsg. clear m.partial because the
		// assistant's text is now part of the appended ContentBlock —
		// leaving the live buffer would double-render the same text. Dedupe
		// by ID so a turnDoneMsg replacement followed by a late-arriving
		// live msg doesn't double-add.
		if msg.Msg.ID != "" {
			for _, existing := range m.messages {
				if existing.ID == msg.Msg.ID {
					return m, m.waitLiveMsg()
				}
			}
		}
		m.messages = append(m.messages, msg.Msg)
		m.partial = ""
		flushCmd := m.flush()
		return m, tea.Batch(flushCmd, m.waitLiveMsg())

	case warningMsg:
		// transient notice from the loop (stream retry, watchdog stall).
		// Show the latest one; arm a fade tick to clear it. Re-arm the
		// channel pump so subsequent notices also surface.
		m.warning = msg.Text
		m.warningExpires = time.Now().Add(warningTTL)
		return m, tea.Batch(warningFadeCmd(), m.waitWarn())

	case warningFadeMsg:
		// only clear if no newer warning has bumped the expiry forward.
		if !m.warningExpires.IsZero() && !time.Now().Before(m.warningExpires) {
			m.warning = ""
			m.warningExpires = time.Time{}
		}
		return m, nil

	case loaderTickMsg:
		// Only animate while a turn or async compact is in flight. Letting the
		// tick die when we leave streaming/compacting keeps idle terminals quiet.
		if m.state != StateStreaming && !m.compacting {
			return m, nil
		}
		m.loaderFrame++
		return m, loaderTickCmd()

	case compactDoneMsg:
		m.compacting = false
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.state = StateError
			return m, nil
		}
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(/compact done)"}},
		})
		return m, m.flush()

	case turnDoneMsg:
		m.cancelRun = nil
		if msg.err != nil {
			m.state = StateError
			m.lastErr = msg.err.Error()
		} else {
			m.messages = msg.result.Messages
			m.partial = ""
			m.state = StateIdle
		}
		flushCmd := m.flush()
		// kick off the top-bar cost flash when a fresh event landed. Diff
		// the call-count against the previous turn so multi-iteration loops
		// fold all their per-iteration events into one visible delta.
		costCmd := m.maybeStartCostFlash()
		// drain one queued follow-up per turn so the TUI stays responsive
		// between fires. Only when last turn didn't error.
		if msg.err == nil && len(m.queue) > 0 && m.eng != nil {
			nxt := m.queue[0]
			m.queue = m.queue[1:]
			nm, runCmd := m.submit(nxt)
			return nm, tea.Batch(flushCmd, costCmd, runCmd)
		}
		return m, tea.Batch(flushCmd, costCmd)

	case costTickMsg:
		if m.costFlashFrame >= m.costFlashUntil {
			m.costFlashUntil = 0
			return m, nil
		}
		m.costFlashFrame++
		return m, costTickCmd()

	case introTickMsg:
		if !m.introActive {
			return m, nil
		}
		// build frames on first tick when width is finally known. If width
		// still hasn't arrived (initial WindowSizeMsg pending), just rearm.
		if m.introFrames == nil {
			if m.width <= 0 {
				return m, introTickCmd()
			}
			m.introFrames = introFrames(m.introStyle, m.width)
			if len(m.introFrames) == 0 {
				m.introActive = false
				return m, nil
			}
		}
		m.introIdx++
		if m.introIdx >= len(m.introFrames) {
			m.introActive = false
			m.introFrames = nil
			return m, nil
		}
		return m, introTickCmd()

	case openPaletteMsg:
		if m.cmds != nil {
			// stage "/" in the main input so user sees a live query line.
			// palette mirrors filter from input value after each keystroke.
			if !strings.HasPrefix(m.input.Value(), "/") {
				m.input.SetValue("/")
				m.input.CursorEnd()
			}
			m.palette.Show(strings.TrimPrefix(m.input.Value(), "/"))
		}
		return m, nil

	case PaletteSelectMsg:
		switch msg.Kind {
		case EntrySkill:
			// stage "/<skill-name>" in the input bar without submitting —
			// keeps invocation discretionary; user can edit or hit enter.
			m.input.SetValue("/" + msg.Name)
			m.input.CursorEnd()
			return m, nil
		default:
			// command: synthesize a "/name" submit so the same dispatch
			// path runs as if the user typed it.
			m.input.SetValue("/" + msg.Name)
			return m.handleSubmit()
		}

	case PaletteDismissedMsg:
		// clear the slash-query staged in the input on esc — the user
		// cancelled the palette, no reason to leave "/foo" behind.
		if strings.HasPrefix(m.input.Value(), "/") {
			m.input.Reset()
		}
		return m, nil

	case AtPickerSelectMsg:
		// replace last `@partial` with the picked path. textarea exposes
		// only column-cursor, not row+col SetCursor, so we set the value
		// and land the cursor at end of buffer.
		val := m.input.Value()
		atIdx := strings.LastIndex(val, "@")
		if atIdx < 0 {
			m.input.SetValue(val + msg.Path)
		} else {
			m.input.SetValue(val[:atIdx] + msg.Path)
		}
		return m, nil

	case AtPickerDismissedMsg:
		return m, nil

	case HistorySelectMsg:
		// paste into the main input; user can edit then submit.
		m.input.SetValue(msg.Text)
		m.input.CursorEnd()
		return m, nil

	case HistoryDismissedMsg:
		return m, nil

	case openTreeMsg:
		if m.tree != nil {
			m.tree.LoadMessages(m.messages, m.currentLeafID())
			newT, cmd := m.tree.Update(ToggleSessionTreeMsg{})
			m.tree = newT
			return m, cmd
		}
		return m, nil

	case openResumeMsg:
		if m.resume != nil {
			newR, cmd := m.resume.Update(ToggleResumePickerMsg{})
			m.resume = newR
			return m, cmd
		}
		return m, nil

	case ResumeSelectMsg:
		if err := m.side().OpenSession(msg.ID); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case ResumeDismissedMsg:
		return m, nil

	case SessionForkMsg:
		if err := m.side().ForkSession(msg.FromID); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case SessionCloneMsg:
		if err := m.side().CloneSession(); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case SessionSwitchMsg:
		// F5 scope: in-place leaf switching needs deeper rollout refactor.
		// Keep the cursor selection client-side; user can /fork to materialize.
		return m, nil

	case openCostMsg:
		if m.isLocalProvider() {
			m.lastErr = "cost monitor hidden for local provider"
			return m, nil
		}
		if m.costPane == nil {
			m.costPane = NewCostPane(m.costs)
		}
		newC, cmd := m.costPane.Update(ToggleCostPaneMsg{})
		m.costPane = newC
		return m, cmd

	case openLoginMsg:
		if m.loginPane == nil {
			m.loginPane = NewLoginPane(m.side())
		}
		newL, cmd := m.loginPane.Update(ToggleLoginPaneMsg{})
		m.loginPane = newL
		return m, cmd

	case openEffortMsg:
		if m.effortPane == nil {
			m.effortPane = NewEffortPane()
		}
		m.effortPane.Show(m.thinking)
		return m, nil

	case openSettingsMsg:
		if m.settingsPane == nil {
			m.settingsPane = NewSettingsPane()
		}
		m.settingsPane.Show(m.verbose, m.showThoughts, m.showNudges, m.compact, m.showContextBar)
		return m, nil

	case settingsToggleMsg:
		// each toggle applies live + persists; side handles all three.
		var err error
		switch msg.key {
		case "verbose":
			err = m.side().SetVerbose(msg.value)
		case "show_thoughts":
			err = m.side().SetShowThoughts(msg.value)
		case "show_nudges":
			err = m.side().SetShowNudges(msg.value)
		case "compact":
			err = m.side().SetCompact(msg.value)
		case "show_context_bar":
			err = m.side().SetShowContextBar(msg.value)
		}
		if err != nil && m.state != StateStreaming {
			// don't kill an in-flight turn over a persist hiccup; surface the
			// error only when idle.
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case loginActionDoneMsg:
		// async login/logout finished — let the pane absorb it even if a
		// key in the meantime closed the pane (so busy flag clears).
		if m.loginPane != nil {
			newL, cmd := m.loginPane.Update(msg)
			m.loginPane = newL
			return m, cmd
		}
		return m, nil

	case openProviderMsg:
		if m.picker == nil {
			return m, nil
		}
		// resize to current frame so columns aren't 0-width on first open
		if m.width > 0 && m.height > 0 {
			m.picker.SetSize(m.width-4, m.height-4)
		}
		return m, m.picker.Show()

	case PickedMsg:
		if err := m.side().SwitchProviderModel(msg.Provider, msg.Model); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
			return m, nil
		}
		// persist for next launch; non-fatal if it fails (e.g. read-only fs)
		if perr := PersistPick("", msg.Provider, msg.Model); perr != nil {
			m.lastErr = "saved live but persist failed: " + perr.Error()
			m.state = StateError
		}
		return m, nil

	case PickerDismissedMsg:
		return m, nil

	case PickerLoginRequestedMsg:
		// picker hit an auth error and user pressed ctrl+l. Open the login
		// pane scoped to the failing provider so they can paste a key inline.
		if m.loginPane != nil {
			m.loginPane.Show()
			m.loginPane.SelectProvider(msg.Provider)
		}
		return m, nil

	case effortPickedMsg:
		v := string(msg)
		if err := m.side().SetThinking(v); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
			return m, nil
		}
		m.thinking = v
		m.effortPane.SetCurrent(v)
		return m, nil

	case openWorkspaceMsg:
		// slice 3B handles workspace; still a no-op.
		return m, nil
	case openHiveMsg:
		if m.agentView != nil {
			m.agentView.Open()
			return m, m.agentView.Init()
		}
		return m, nil
	case CloseAgentViewMsg:
		if m.agentView != nil {
			m.agentView.Close()
		}
		return m, nil
	case AttachSessionMsg:
		// attach defers to the side adapter; AgentView returns the id and the
		// outer app routes it to /attach. For now: just close the pane.
		if m.agentView != nil {
			m.agentView.Close()
		}
		return m, nil
	case agentTickMsg:
		if m.agentView != nil && m.agentView.IsOpen() {
			var cmd tea.Cmd
			m.agentView, cmd = m.agentView.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// classic readline chords must reach the textinput when the buffer has
	// content. Pane/quit shortcuts that overlap with editing chords
	// (ctrl+w word-back, ctrl+k kill-to-end, ctrl+h backspace, ctrl+t
	// transpose) only fire on an empty buffer. ctrl+d is handled globally
	// in Update (double-press to quit) so it never reaches handleKey.
	inputEmpty := m.input.Value() == ""
	keyStr := msg.String()
	// agent view: open with Left on empty input, close with Right/esc.
	// claim all keys while open so cursor moves never leak to the
	// textarea behind the overlay; delegate everything to AgentView.Update.
	if m.agentView != nil && m.agentView.IsOpen() {
		if keyStr == "right" {
			m.agentView.Close()
			return m, nil
		}
		var cmd tea.Cmd
		m.agentView, cmd = m.agentView.Update(msg)
		return m, cmd
	}
	if keyStr == "left" && inputEmpty && m.state == StateIdle {
		return m, func() tea.Msg { return openHiveMsg{} }
	}
	editingChord := keyStr == "ctrl+w" || keyStr == "ctrl+k" ||
		keyStr == "ctrl+h" || keyStr == "ctrl+t"
	switch {
	case key.Matches(msg, m.keys.Cancel):
		if m.state == StateStreaming {
			if m.cancelRun != nil {
				m.cancelRun()
				m.cancelRun = nil
			}
			m.state = StateIdle
		}
		return m, nil
	case key.Matches(msg, m.keys.FollowUp):
		return m.handleFollowUp()
	case key.Matches(msg, m.keys.ImagePaste):
		return m.handleImagePaste()
	case key.Matches(msg, m.keys.Submit):
		// state-dependent: idle = submit, streaming = steer. Slash commands
		// always route to handleSubmit so AllowDuringRun ones (/settings,
		// /effort, /model, …) work mid-stream instead of being captured as
		// steer text.
		if m.state == StateStreaming && !strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
			return m.handleSteer()
		}
		return m.handleSubmit()
	case key.Matches(msg, m.keys.ProviderPick):
		return m, func() tea.Msg { return openProviderMsg{} }
	case key.Matches(msg, m.keys.WorkspaceTog):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openWorkspaceMsg{} }
	case key.Matches(msg, m.keys.HiveOpen):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openHiveMsg{} }
	case key.Matches(msg, m.keys.SessionTree):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openTreeMsg{} }
	case key.Matches(msg, m.keys.CostOpen):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openCostMsg{} }
	case key.Matches(msg, m.keys.SlashPalette):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openPaletteMsg{} }
	case key.Matches(msg, m.keys.HistorySearch):
		// ctrl+r opens reverse history search with the current buffer as
		// initial filter — fzf-style.
		m.history.Show(m.input.Value())
		return m, nil
	case key.Matches(msg, m.keys.CavemanCycle):
		m.caveLvl = cycleCaveman(m.caveLvl)
		return m, nil
	case key.Matches(msg, m.keys.ThinkingCycle):
		m.thinking = cycleThinking(m.thinking)
		if m.eng != nil {
			m.eng.Cfg.Thinking = m.thinking
		}
		_ = PersistSetting("", "thinking", m.thinking)
		return m, nil
	case key.Matches(msg, m.keys.ModeCycle):
		prov := ""
		if m.eng != nil {
			prov = m.eng.Cfg.DefaultProvider
		}
		m.mode = cycleMode(m.mode, prov)
		if m.eng != nil {
			m.eng.Cfg.Mode = m.mode
		}
		return m, nil
	}
	// scroll keys are no-ops now — terminal handles native scroll back over
	// printed messages. PageUp/PageDown/Up/Down/ctrl+s used to drive the
	// viewport widget; with View() rendering only the live region they fall
	// through to the textarea (Up/Down move the cursor between input lines).
	// `?` toggles the help line when the input is empty — keeps the
	// chrome silent by default.
	if msg.String() == "?" && m.input.Value() == "" {
		m.showHelp = !m.showHelp
		return m, nil
	}
	// `/` while idle on an empty input opens the palette. The "/" itself
	// flows into the input bar so the user sees what they're typing;
	// subsequent chars also land in the input and the palette filter
	// mirrors via SetFilter (see KeyMsg branch in Update).
	if msg.String() == "/" && m.input.Value() == "" && m.state == StateIdle && !m.palette.Active {
		m.palette.Show("")
		// fall through to let the textinput consume "/"
	}
	// `@` while idle opens the fuzzy file picker.
	if msg.String() == "@" && m.state == StateIdle && !m.atpicker.Active {
		// insert the `@` literally so dismissing leaves it in place
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.atpicker = NewAtPicker(m.cwd)
		m.atpicker.SetWidth(m.width)
		m.atpicker.Active = true
return m, tea.Batch(cmd)
	}
	// up/down cycle through past prompts (fish-style). Single-line buffers
	// route into cycling on up; once cycling, both arrows continue regardless
	// of line count (history entries may themselves be multi-line). Multi-
	// line buffers leave up/down alone so they navigate textarea rows.
	if (keyStr == "up" || keyStr == "down") && m.state == StateIdle {
		if m.cycleActive || (keyStr == "up" && m.input.LineCount() == 1) {
			if !m.cycleActive {
				m.cycleEntries = LoadHistory()
				m.cycleStash = m.input.Value()
				m.cycleIdx = -1
				m.cycleActive = true
			}
			if len(m.cycleEntries) == 0 {
				m.cycleActive = false
				return m, nil
			}
			if keyStr == "up" {
				if m.cycleIdx+1 < len(m.cycleEntries) {
					m.cycleIdx++
				}
				m.input.SetValue(m.cycleEntries[m.cycleIdx])
				m.input.CursorEnd()
				return m, nil
			}
			// down: walk back toward stash; past it ends the cycle.
			m.cycleIdx--
			if m.cycleIdx < 0 {
				m.cycleActive = false
				m.input.SetValue(m.cycleStash)
				m.input.CursorEnd()
				return m, nil
			}
			m.input.SetValue(m.cycleEntries[m.cycleIdx])
			m.input.CursorEnd()
			return m, nil
		}
	}
	// any other key while cycling resets the cycle — the landed-on entry
	// becomes the new base buffer for normal editing.
	if m.cycleActive {
		m.cycleActive = false
		m.cycleEntries = nil
		m.cycleStash = ""
		m.cycleIdx = -1
	}
	// Tab: try path completion on the cursor line. textarea exposes only
	// column-cursor, so we operate on the end of the current value when
	// the cursor is at end; otherwise fall through to default behavior.
	if msg.String() == "tab" && m.state == StateIdle {
		val := m.input.Value()
		// only auto-complete when buffer is a single line and cursor at end —
		// the 95% case for path completion. multi-line tabs fall through.
		if m.input.LineCount() == 1 {
			start := strings.LastIndexAny(val, " \t") + 1
			partial := val[start:]
			if partial != "" {
				dir := filepath.Dir(partial)
				if dir == "." || dir == "" {
					dir = m.cwd
				} else if !filepath.IsAbs(dir) {
					dir = filepath.Join(m.cwd, dir)
				}
				base := filepath.Base(partial)
				cands := CompletionCandidates(dir, base)
				if len(cands) > 0 {
					completion := LongestCommonPrefix(cands)
					if completion != base {
						add := completion[len(base):]
						m.input.SetValue(val + add)
						return m, nil
					}
				}
			}
		}
		// no path completion — let textinput accept the ghost suggestion.
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleImagePaste reads the system clipboard and stages an image for the
// next submit. Only fires when idle — staging mid-stream would race with the
// running engine's message slice. Failure (no image, headless clipboard)
// surfaces via the standard error line.
func (m Model) handleImagePaste() (tea.Model, tea.Cmd) {
	if m.state != StateIdle {
		return m, nil
	}
	img, err := ReadClipboardImage()
	if err != nil {
		m.lastErr = "clipboard: " + err.Error()
		m.state = StateError
		return m, nil
	}
	m.pendingImage = img
	m.messages = append(m.messages, types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{{
			Type: types.BlockText,
			Text: "(image staged: " + bytesHuman(len(img)) + ")",
		}},
	})
	return m, m.flush()
}

// bytesHuman renders a byte count as B/KiB/MiB. Used for staged-image hints.
func bytesHuman(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%d KiB", n/1024)
	}
	return fmt.Sprintf("%d MiB", n/(1024*1024))
}

// handleFollowUp pushes the current input onto the follow-up queue. Fires
// regardless of state — queued runs kick off as the current turn finishes.
func (m Model) handleFollowUp() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.queue = append(m.queue, text)
	m.messages = append(m.messages, types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "(queued: " + text + ")"}},
	})
	return m, m.flush()
}

// handleSteer pushes the current input into the engine's SteerCh so the
// running Run loop picks it up between iterations. Drops with a note if
// the channel is full or the engine isn't wired for steering.
func (m Model) handleSteer() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	if m.eng == nil || m.eng.SteerCh == nil {
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(steer dropped: no channel)"}},
		})
		return m, m.flush()
	}
	select {
	case m.eng.SteerCh <- text:
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(steering: " + text + ")"}},
		})
	default:
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(steer dropped: queue full)"}},
		})
	}
	return m, m.flush()
}

// handleSubmit dispatches the current input. A leading "/" routes to the
// command registry; anything else feeds the LLM via submit(). Empty input
// or non-idle state are silent no-ops.
func (m Model) handleSubmit() (tea.Model, tea.Cmd) {
	// allow recovery from StateError: clear the error and proceed. blocking
	// here was the root cause of "Enter does nothing after max-iter abort".
	if m.state == StateError {
		m.state = StateIdle
		m.lastErr = ""
	}
	if m.state != StateIdle {
		// mid-run slash commands: allow read-only ones (model, cost, tree,
		// help, settings, effort) so the user can flip settings or open a
		// picker without waiting for the turn to finish. Anything else is a
		// no-op with a transient hint.
		text := strings.TrimSpace(m.input.Value())
		if !strings.HasPrefix(text, "/") {
			return m, nil
		}
		name := strings.SplitN(strings.TrimPrefix(text, "/"), " ", 2)[0]
		if m.cmds != nil {
			if c, ok := m.cmds.Get(name); ok && c.AllowDuringRun {
				m.input.Reset()
				// preserve streaming state across slashFail — runSlash flips to
				// StateError on bad input which would silently kill the turn.
				prev := m.state
				newM, cmd := m.runSlash(text)
				if mm, ok := newM.(Model); ok && mm.state == StateError {
					mm.state = prev
					mm.lastErr = ""
					return mm, cmd
				}
				return newM, cmd
			}
		}
		m.input.Reset()
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(/" + name + " unavailable while bee is running)"}},
		})
		return m, m.flush()
	}
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	// record every accepted submission for ctrl+r reverse search.
	AppendHistory(text)

	// inline shell: !cmd forwards to LLM with output appended, !!cmd is silent.
	if cmd, silent, isInline := parseInlinePrefix(text); isInline {
		if m.eng == nil {
			return m, nil
		}
		res := runInlineShell(m.ctx, m.eng.Tools, cmd, silent)
		payload := formatInlineShell(cmd, res.Output, res.IsError)
		if silent {
			// local-only: record the styled shell exec, no engine turn.
			m.messages = append(m.messages, types.Message{
				Role:    types.RoleUser,
				Content: []types.ContentBlock{{Type: types.BlockText, Text: payload}},
			})
			return m, m.flush()
		}
		// non-silent: submit the shell record as the user turn so the LLM sees
		// the full cmd+output once and the scrollback shows a single styled card.
		return m.submit(payload)
	}

	if strings.HasPrefix(text, "/") {
		return m.runSlash(text)
	}
	return m.submit(text)
}

// runSlash parses "/name args…", looks up the command, runs it, and
// renders the result. Unknown commands surface as a transient error.
func (m Model) runSlash(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(parts) == 0 {
		return m, nil
	}
	if m.cmds == nil {
		m.lastErr = "no command registry"
		m.state = StateError
		return m, nil
	}
	c, ok := m.cmds.Get(parts[0])
	if !ok {
		m.lastErr = "unknown command /" + parts[0]
		m.state = StateError
		return m, nil
	}

	// /compact runs async with a loader animation so the LLM summarization
	// call doesn't freeze the UI. State stays StateIdle; m.compacting drives
	// the loader tick. See compactDoneMsg handler in Update.
	if parts[0] == "compact" {
		if m.compacting {
			return m, nil // already running
		}
		m.compacting = true
		m.loaderFrame = 0
		side := m.side()
		ctx := m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		return m, tea.Batch(
			loaderTickCmd(),
			func() tea.Msg {
				return compactDoneMsg{err: side.Compact(ctx)}
			},
		)
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := c.Run(ctx, parts[1:], m.side())
	if err != nil {
		m.lastErr = err.Error()
		m.state = StateError
		return m, nil
	}
	// /quit asks the TUI to exit — bubble that up as tea.Quit.
	if m.quitRequested {
		return m, tea.Quit
	}
	// /tree asks to open the modal — dispatch the open message.
	if m.treeRequested {
		m.treeRequested = false
		return m, func() tea.Msg { return openTreeMsg{} }
	}
	// /resume asks to open the resume picker.
	if m.resumeRequested {
		m.resumeRequested = false
		return m, func() tea.Msg { return openResumeMsg{} }
	}
	// /cost asks to open the cost modal.
	if m.costRequested {
		m.costRequested = false
		return m, func() tea.Msg { return openCostMsg{} }
	}
	// /login (no args) asks to open the login pane.
	if m.loginRequested {
		m.loginRequested = false
		return m, func() tea.Msg { return openLoginMsg{} }
	}
	// /effort (no args) asks to open the effort picker.
	if m.effortRequested {
		m.effortRequested = false
		return m, func() tea.Msg { return openEffortMsg{} }
	}
	// /settings asks to open the settings pane.
	if m.settingsRequested {
		m.settingsRequested = false
		return m, func() tea.Msg { return openSettingsMsg{} }
	}
	// /model (no args) asks to open the provider+model picker.
	if m.pickerRequested {
		m.pickerRequested = false
		return m, func() tea.Msg { return openProviderMsg{} }
	}
	if out == "" {
		// pure side-effect: echo a brief confirmation into scrollback.
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(/" + parts[0] + " done)"}},
		})
		return m, m.flush()
	}
	// command produced text — render it as assistant output, not a LLM turn.
	m.messages = append(m.messages, types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: out}},
	})
	return m, m.flush()
}

// submit records the user message locally and kicks off engine.Run in a goroutine.
// The result comes back via turnDoneMsg.
func (m Model) submit(text string) (tea.Model, tea.Cmd) {
	m.state = StateStreaming
	m.partial = ""
	m.loaderFrame = 0

	// build content blocks: text first, then a pending image if staged.
	content := []types.ContentBlock{{Type: types.BlockText, Text: text}}
	if len(m.pendingImage) > 0 {
		content = append(content, types.ContentBlock{
			Type:      types.BlockImage,
			MediaType: "image/png",
			Data:      m.pendingImage,
		})
		m.pendingImage = nil
	}

	// optimistic user message in scrollback
	m.messages = append(m.messages, types.Message{
		Role:    types.RoleUser,
		Content: content,
	})
	userFlush := m.flush()

	if m.eng == nil {
		// no engine wired (tests): synthesize an echo turn so state still advances
		echo := types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(no engine: " + text + ")"}},
		}
		merged := append([]types.Message{}, m.messages...)
		merged = append(merged, echo)
		return m, tea.Batch(
			userFlush,
			func() tea.Msg { return turnDoneMsg{result: loop.RunResult{Messages: merged}} },
			loaderTickCmd(),
		)
	}

	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancelRun = cancel
	eng := m.eng
	// seed prior turns into engine so the model retains context across
	// submits. exclude the optimistic user msg we just appended — engine
	// adds its own properly-IDed copy.
	if n := len(m.messages); n > 0 {
		eng.InitialMessages = append([]types.Message{}, m.messages[:n-1]...)
	} else {
		eng.InitialMessages = nil
	}
	return m, tea.Batch(
		userFlush,
		func() tea.Msg {
			res, err := eng.RunWithContent(ctx, content)
			return turnDoneMsg{result: res, err: err}
		},
		loaderTickCmd(),
	)
}

// Run builds and runs the bubbletea program with the given engine.
// Blocks until the program exits (Ctrl+C or tea.Quit). Uses the default
// (built-in) slash command set.
func Run(ctx context.Context, eng *loop.Engine) error {
	return RunWithCommands(ctx, eng, nil)
}

// RunWithCommands is Run with a caller-provided command registry. Pass nil
// to get the built-in set. Uses DefaultKeyMap — callers wanting user overrides
// should use RunWithCommandsAndKeyMap.
func RunWithCommands(ctx context.Context, eng *loop.Engine, reg *commands.Registry) error {
	return RunWithCommandsAndKeyMap(ctx, eng, reg, DefaultKeyMap())
}

// RunWithCommandsAndKeyMap is RunWithCommands plus a caller-supplied keymap.
// Pass DefaultKeyMap() to keep stock bindings.
func RunWithCommandsAndKeyMap(ctx context.Context, eng *loop.Engine, reg *commands.Registry, km KeyMap) error {
	cwd := ""
	modelName := ""
	scope := ""
	lvl := caveman.Default
	if eng != nil {
		cwd = eng.Cwd
		modelName = eng.Cfg.DefaultModel
		scope = eng.Cfg.Sandbox.Scope
	}
	m := NewModel(eng, cwd, modelName, scope, lvl)
	if reg != nil {
		m = m.WithCommands(reg)
	}
	// thread the engine's skills registry into the palette so /<…> lists
	// both commands and skills in one fuzzy view.
	if eng != nil && eng.Skills != nil {
		m = m.WithSkills(eng.Skills)
	}
	m = m.WithKeyMap(km)
	// intro: cfg.ShowBanner gates the non-blocking startup animation;
	// BEE_BANNER picks the variant (handled inside WithIntro/introFrames).
	if eng != nil && eng.Cfg.ShowBanner {
		m = m.WithIntro(ParseIntroStyle(os.Getenv("BEE_BANNER")))
	}
	// verbose: env wins over cfg (CLI/env path); cfg persists across launches.
	verbose := os.Getenv("BEE_VERBOSE") != ""
	if !verbose && eng != nil {
		verbose = eng.Cfg.Verbose
	}
	if verbose {
		m = m.WithVerbose(true)
	}
	// show-thoughts: cfg-driven; default true even when eng is nil (tests).
	if eng != nil {
		m = m.WithShowThoughts(eng.Cfg.ShowThoughts)
	} else {
		m = m.WithShowThoughts(true)
	}
	// show-nudges: cfg-driven; default false hides loop recovery turns.
	if eng != nil {
		m = m.WithShowNudges(eng.Cfg.ShowNudges)
	}
	// compact: env wins over cfg; cfg persists across launches.
	compact := os.Getenv("BEE_COMPACT") != ""
	if !compact && eng != nil {
		compact = eng.Cfg.Compact
	}
	if compact {
		m = m.WithCompact(true)
	}
	// show-context-bar: cfg-driven; default false (hex glyph carries fill).
	if eng != nil {
		m = m.WithShowContextBar(eng.Cfg.ShowContextBar)
	}
	// hand the engine's stream channel to the model so deltas land in the
	// bubbletea Update loop instead of corrupting the alt-screen.
	if eng != nil && eng.StreamCh != nil {
		m = m.WithStreamCh(eng.StreamCh)
	}
	if eng != nil && eng.LiveMsgCh != nil {
		m = m.WithLiveMsgCh(eng.LiveMsgCh)
	}
	if eng != nil && eng.WarnCh != nil {
		m = m.WithWarnCh(eng.WarnCh)
	}
	if eng != nil && eng.Costs != nil {
		m = m.WithCostTracker(eng.Costs)
	}
	// resume: seed scrollback from prior session
	if eng != nil && len(eng.InitialMessages) > 0 {
		m = m.WithInitialMessages(eng.InitialMessages)
	}
	m.ctx = ctx
	// Always inline: View() owns only the live region (status + partial +
	// input), finalized messages get pushed up via tea.Println, terminal
	// handles native scroll/select/copy across history.
	// Enable xterm modifyOtherKeys so shift+enter/ctrl+enter arrive as
	// distinct chords (translated to ctrl+j by csiTranslator) instead of
	// collapsing to a bare \r that the Submit binding would swallow.
	input, restoreKeys := InstallModifyOtherKeys(os.Stdout)
	defer restoreKeys()
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input))
	_, err := p.Run()
	return err
}

// currentLeafID returns the id of the last message in scrollback, or "" if empty.
func (m *Model) currentLeafID() string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1].ID
}

// cycleThinking rotates Auto → Off → Low → Medium → High → Auto.
func cycleThinking(t string) string {
	switch llm.ParseThinking(t) {
	case llm.ThinkingAuto:
		return string(llm.ThinkingOff)
	case llm.ThinkingOff:
		return string(llm.ThinkingLow)
	case llm.ThinkingLow:
		return string(llm.ThinkingMedium)
	case llm.ThinkingMedium:
		return string(llm.ThinkingHigh)
	default:
		return string(llm.ThinkingAuto)
	}
}

// cycleMode rotates plan → auto → edit → plan. Local providers skip the
// auto stop — the classifier wastes tokens on slow on-host models and the
// extra round-trip is more painful than the value of intent-guessing.
// Default landing on edit when input is empty/unknown so shift+tab from a
// fresh session behaves predictably.
func cycleMode(mode, provider string) string {
	local := config.IsLocalProvider(provider)
	switch loop.ParseMode(mode) {
	case loop.ModePlan:
		if local {
			return string(loop.ModeEdit)
		}
		return string(loop.ModeAuto)
	case loop.ModeAuto:
		return string(loop.ModeEdit)
	case loop.ModeEdit:
		return string(loop.ModePlan)
	default:
		return string(loop.ModeEdit)
	}
}

// cycleCaveman rotates Off → Lite → Full → Ultra → Off.
func cycleCaveman(l caveman.Level) caveman.Level {
	switch l {
	case caveman.Off:
		return caveman.Lite
	case caveman.Lite:
		return caveman.Full
	case caveman.Full:
		return caveman.Ultra
	default:
		return caveman.Off
	}
}

// isLocalProvider returns true when the active engine targets an on-host
// provider (ollama / lmstudio / etc). Used to hide cost UI and skip the
// auto-mode classifier — local runs have no $ to track and no need to
// burn extra tokens classifying intent.
func (m Model) isLocalProvider() bool {
	if m.eng == nil {
		return false
	}
	return config.IsLocalProvider(m.eng.Cfg.DefaultProvider)
}
