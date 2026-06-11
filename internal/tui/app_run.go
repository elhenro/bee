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
	return RunWithCommandsKeyMapApprover(ctx, eng, reg, km, nil)
}

// RunWithCommandsKeyMapApprover is RunWithCommandsAndKeyMap plus the channel
// approver that surfaces dangerous-command prompts in the modal. Pass nil for
// the legacy no-gating behavior.
func RunWithCommandsKeyMapApprover(ctx context.Context, eng *loop.Engine, reg *commands.Registry, km KeyMap, app *Approver) error {
	return RunSeeded(ctx, eng, reg, km, app, "")
}

// RunSeeded is RunWithCommandsKeyMapApprover plus an optional seed prompt that
// auto-submits one turn on startup (skill dispatch into the TUI). Pass "" for
// no auto-submit.
func RunSeeded(ctx context.Context, eng *loop.Engine, reg *commands.Registry, km KeyMap, app *Approver, seed string) error {
	return RunSeededAsker(ctx, eng, reg, km, app, nil, seed)
}

// RunSeededAsker is RunSeeded plus the ask_user picker adapter. Pass nil to
// leave ask_user auto-resolving to the recommended option.
func RunSeededAsker(ctx context.Context, eng *loop.Engine, reg *commands.Registry, km KeyMap, app *Approver, asker *Asker, seed string) error {
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
	if app != nil {
		m = m.WithApprover(app)
	}
	if asker != nil {
		m = m.WithAsker(asker)
	}
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
	if eng != nil {
		m = m.WithShowBanner(eng.Cfg.ShowBanner).WithShowLoader(eng.Cfg.ShowLoader).
			WithShowLoaderIn(eng.Cfg.ShowLoaderIn).
			WithShowLoaderOut(eng.Cfg.ShowLoaderOut).
			WithShowLoaderRate(eng.Cfg.ShowLoaderRate)
	} else {
		m = m.WithShowBanner(true).WithShowLoader(true).
			WithShowLoaderIn(true).WithShowLoaderOut(true).WithShowLoaderRate(true)
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
	// show-recap: cfg-driven; default false (no side call).
	if eng != nil {
		m = m.WithShowRecap(eng.Cfg.ShowRecap)
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
	// highlight: cfg-driven; default true. Skip default-call when eng is nil
	// so tests stay on the ctor default already set to true.
	if eng != nil {
		m = m.WithHighlight(eng.Cfg.Highlight)
	}
	// shell-bang-silent: cfg-driven; default true so `!cmd` runs locally
	// without LLM forwarding. Skip when eng nil to keep test defaults.
	if eng != nil {
		m = m.WithShellBangSilent(eng.Cfg.ShellBangSilent)
	}
	// top-bar chrome: cfg-driven; default true preserves the original row.
	if eng != nil {
		m = m.WithShowBee(eng.Cfg.ShowBee).
			WithShowContextPct(eng.Cfg.ShowContextPct).
			WithShowModel(eng.Cfg.ShowModel).
			WithShowCwd(eng.Cfg.ShowCwd).
			WithShowEffort(eng.Cfg.ShowEffort).
			WithShowTurnTimer(eng.Cfg.ShowTurnTimer).
			WithShowGitBranch(eng.Cfg.ShowGitBranch).
			WithShowTotalTokens(eng.Cfg.ShowTotalTokens)
	}
	// hand the engine's stream channel to the model so deltas land in the
	// bubbletea Update loop instead of corrupting the alt-screen.
	if eng != nil && eng.StreamCh != nil {
		m = m.WithStreamCh(eng.StreamCh)
	}
	if eng != nil && eng.ThinkCh != nil {
		m = m.WithThinkCh(eng.ThinkCh)
	}
	if eng != nil && eng.ProgressCh != nil {
		m = m.WithProgressCh(eng.ProgressCh)
	}
	if eng != nil && eng.LiveMsgCh != nil {
		m = m.WithLiveMsgCh(eng.LiveMsgCh)
	}
	if eng != nil && eng.WarnCh != nil {
		m = m.WithWarnCh(eng.WarnCh)
	}
	if eng != nil {
		wc := eng.Cfg.Watchdog
		if os.Getenv("BEE_WATCHDOG_OFF") != "" {
			wc.Enabled = false
		}
		m = m.WithWatchdog(wc.Enabled, wc.Stall(), wc.Resumes())
	}
	if eng != nil && eng.Costs != nil {
		m = m.WithCostTracker(eng.Costs)
	}
	// resume: seed scrollback from prior session
	if eng != nil && len(eng.InitialMessages) > 0 {
		m = m.WithInitialMessages(eng.InitialMessages)
	}
	if seed != "" {
		m = m.WithSeedPrompt(seed)
	}
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
		engRef := eng // capture for the closure so probes pick up live mode changes
		startUpdateChecker(checkerCtx, p, updateCheckConfig{
			mode:     func() string { return engRef.Cfg.UpdateCheck },
			repo:     eng.Cfg.UpdateRepo,
			branch:   eng.Cfg.UpdateBranch,
			interval: time.Hour,
		})
	}
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

// cycleRole rotates worker → scout → queen → worker. No provider special-case:
// each role bundles its own tool surface + reasoning budget, so there's no auto
// classifier stop left to skip. Unknown input lands on worker (the default).
func cycleRole(role string) string {
	switch loop.ParseRole(role) {
	case loop.RoleWorker:
		return string(loop.RoleScout)
	case loop.RoleScout:
		return string(loop.RoleQueen)
	case loop.RoleQueen:
		return string(loop.RoleWorker)
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
