package tui

import (
	"context"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

// RunSeededAsker builds and runs the bubbletea program with the given engine,
// slash-command registry (nil = built-ins), keymap, dangerous-command approver
// and ask_user picker adapter (nil = ask_user auto-resolves to the recommended
// option). A non-empty seed auto-submits one turn on startup (skill dispatch
// into the TUI). Blocks until the program exits.
func RunSeededAsker(ctx context.Context, eng *loop.Engine, reg *commands.Registry, km KeyMap, app *Approver, asker *Asker, seed string) error {
	var cwd, modelName, scope string
	if eng != nil {
		cwd, modelName, scope = eng.Cwd, eng.Cfg.DefaultModel, eng.Cfg.Sandbox.Scope
	}
	m := NewModel(eng, cwd, modelName, scope, caveman.Default)
	m.approver, m.asker = app, asker
	if reg != nil {
		m = m.WithCommands(reg)
	}
	// thread the engine's skills registry into the palette so /<…> lists
	// both commands and skills in one fuzzy view.
	if eng != nil && eng.Skills != nil {
		m = m.WithSkills(eng.Skills)
	}
	m.keys = km
	m.approval = NewApprovalModel(*m.styles, km)
	// intro: cfg.ShowBanner gates the non-blocking startup animation;
	// BEE_BANNER picks the variant, BEE_NO_INTRO=1 disables it.
	if eng != nil && eng.Cfg.ShowBanner && os.Getenv("BEE_NO_INTRO") != "1" {
		m.introStyle, m.introActive = ParseIntroStyle(os.Getenv("BEE_BANNER")), true
	}
	m = m.seedFromEngine(eng).WithSeedPrompt(seed)
	// first-run walkthrough: show the welcome gate on a fresh interactive start
	// (not a skill dispatch, not a resume). Deferred past the intro when it
	// plays (activated from onIntroTick); BEE_NO_TUTORIAL suppresses it.
	if eng != nil && !eng.Cfg.TutorialDone && seed == "" &&
		len(eng.InitialMessages) == 0 && os.Getenv("BEE_NO_TUTORIAL") == "" {
		if m.introActive {
			m.tutorialPending = true
		} else {
			m.tutorial = tutorialState{active: true, phase: tutPhaseGate}
		}
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
	if m.approver != nil {
		m.approver.SetProgram(p)
	}
	if m.asker != nil {
		m.asker.SetProgram(p)
	}
	// hourly background probe of the bee repo's main branch. Off when:
	//   - the build wasn't tagged with a real commit (Commit == "" or "dev")
	//   - cfg.UpdateCheck == "off"
	// First probe fires 15s after launch, then once per interval. Findings
	// flow back as updateAvailableMsg (mode "ask") or updateAppliedMsg
	// (mode "auto") through p.Send.
	checkerCtx, cancelChecker := context.WithCancel(ctx)
	defer cancelChecker()
	if eng != nil {
		setUpdateMode(eng.Cfg.UpdateCheck)
		startUpdateChecker(checkerCtx, p, updateCheckConfig{
			mode:     updateMode,
			repo:     eng.Cfg.UpdateRepo,
			branch:   eng.Cfg.UpdateBranch,
			interval: time.Hour,
		})
	}
	_, err := p.Run()
	return err
}

// seedFromEngine mirrors the engine config, channels and cost tracker onto the
// model's display toggles. Without an engine (tests) the model keeps its
// constructor defaults, with the banner and loader figures on. Verbose and
// compact also come from the environment.
func (m Model) seedFromEngine(eng *loop.Engine) Model {
	m.verbose = os.Getenv("BEE_VERBOSE") != ""
	m.compact = os.Getenv("BEE_COMPACT") != ""
	if eng == nil {
		m.showBanner, m.showLoader, m.showLoaderIn, m.showLoaderOut = true, true, true, true
		m.syncStream()
		return m
	}
	c := eng.Cfg
	m.verbose = m.verbose || c.Verbose
	m.compact = m.compact || c.Compact
	m.showBanner, m.showLoader = c.ShowBanner, c.ShowLoader
	m.showLoaderIn, m.showLoaderOut, m.showLoaderRate = c.ShowLoaderIn, c.ShowLoaderOut, c.ShowLoaderRate
	m.showThoughts, m.showNudges, m.showRecap = c.ShowThoughts, c.ShowNudges, c.ShowRecap
	m.showContextBar, m.showCallDetail = c.ShowContextBar, c.ShowCallDetail
	m.highlight, m.shellBangSilent = c.Highlight, c.ShellBangSilent
	// top-bar chrome
	m.showBee, m.showContextPct, m.showModel, m.showCwd = c.ShowBee, c.ShowContextPct, c.ShowModel, c.ShowCwd
	m.showEffort, m.showTurnTimer, m.showGitBranch, m.showTotalTokens = c.ShowEffort, c.ShowTurnTimer, c.ShowGitBranch, c.ShowTotalTokens
	m.loader = c.TUI.Loader
	m = m.WithStatusLine(c.TUI.StatusLine).WithOuterPadLR(c.OuterPadLR)
	// hand the engine's channels to the model so deltas land in the bubbletea
	// Update loop instead of corrupting the alt-screen.
	m.streamCh, m.thinkCh, m.progressCh, m.liveMsgCh, m.warnCh = eng.StreamCh, eng.ThinkCh, eng.ProgressCh, eng.LiveMsgCh, eng.WarnCh
	m.costs = eng.Costs
	wc := c.Watchdog
	if os.Getenv("BEE_WATCHDOG_OFF") != "" {
		wc.Enabled = false
	}
	m.watchdogEnabled, m.watchdogStall, m.watchdogMaxResumes = wc.Enabled, wc.Stall(), wc.Resumes()
	m.syncStream()
	// resume: seed scrollback from prior session
	return m.WithInitialMessages(eng.InitialMessages)
}

// currentLeafID returns the id of the last message in scrollback, or "" if empty.
func (m *Model) currentLeafID() string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1].ID
}

// cycleRole rotates worker → scout → queen → worker. No provider special-case:
// each role bundles its own tool surface + reasoning budget, so there's no auto
// classifier stop left to skip. Unknown input lands on worker (the default).
func cycleRole(role string) string {
	switch loop.ParseRole(role) {
	case loop.RoleWorker:
		return string(loop.RoleScout)
	case loop.RoleScout:
		return string(loop.RoleQueen)
	default:
		return string(loop.RoleWorker)
	}
}

// roleThinking is the reasoning budget baked into each role, mirrored into
// m.thinking for display and into eng.Cfg.Thinking so the engine resolves the
// same value. Kept here (not via loop.RoleThinking) so the TUI controls the
// mirror string without a per-turn config dependency.
func roleThinking(role string) string {
	return string(loop.RoleThinking(loop.ParseRole(role)))
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
