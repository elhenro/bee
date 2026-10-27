// Interactive TUI entry point. Builds the same Engine as `bee run` but
// hands it to the bubbletea program in internal/tui.
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/beehome"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/herdr"
	"github.com/elhenro/bee/internal/jobs"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tui"
	"github.com/elhenro/bee/internal/types"
)

// runTUI wires the full engine the same way runHeadless does, then
// hands control to internal/tui. Returns once the program exits.
func runTUI() { runTUIWithSession("", "") }

// runTUIWithSession is runTUI plus an optional pre-existing session id and an
// optional seed prompt. resumeID reopens a prior rollout in append mode and
// seeds its messages; seedPrompt auto-submits one turn on startup (skill
// dispatch into the TUI). Both empty = fresh interactive session.
func runTUIWithSession(resumeID, seedPrompt string) {
	// Pre-declare bg so lipgloss skips its OSC 11 query — bubbletea owns
	// stdin in altscreen mode and the reply otherwise leaks into the
	// textinput. BEE_THEME=light|dark wins, else parse COLORFGBG from the
	// terminal (iTerm/Ghostty/Terminal.app set it on profile load), else
	// default dark. COLORFGBG fields look like "fg;bg" — bg<7 = dark.
	dark := detectDarkBg()
	lipgloss.SetHasDarkBackground(dark)
	if os.Getenv("COLORFGBG") == "" {
		if dark {
			_ = os.Setenv("COLORFGBG", "15;0")
		} else {
			_ = os.Setenv("COLORFGBG", "0;15")
		}
	}

	cfg := mustConfig("bee", "", "", "")
	// global --caveman flag (stripped in main) lands here via env; pre-validated
	// in stripCavemanFlag, so no re-parse.
	if v := os.Getenv("BEE_CAVEMAN"); v != "" {
		cfg.Caveman = v
	}
	// global --effort flag (stripped in main) lands here via env; pre-validated
	// so no re-parse. Overrides cfg.Thinking so the TUI session opens at the
	// requested reasoning level.
	if v := os.Getenv("BEE_EFFORT"); v != "" {
		cfg.Thinking = v
	}

	prov := mustProvider("bee", cfg)

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	tuiApprover := tui.NewApprover()
	tuiAsker := tui.NewAsker()
	app := approval.NewCache(herdr.WrapApprover(tuiApprover), cfg.Sandbox.CommandAllowlist, PersistAllowlistEntry)
	defer app.Flush()
	// background commands: finished jobs wake the TUI (idle) or ride into the
	// running turn. closed on exit so no process outlives bee.
	jm := jobs.New()
	defer jm.Close()
	ensureFirstRun()
	skillReg := newSkillRegistry()
	toolsFor := func(wcwd string, c config.Config, p llm.Provider, opts ...buildOpt) (*tools.Registry, error) {
		r, err := buildToolsAsker(wcwd, c, p, storeDir, app, tuiAsker, opts...)
		if err == nil {
			registerSkillTools(r, skillReg, c, wcwd)
		}
		return r, err
	}
	reg, err := toolsFor(cwd, cfg, prov, withJobs(jm))
	if err != nil {
		exitf(1, "bee: tools: %v", err)
	}

	// pre-warm the live model catalogue for the active provider so the
	// context-fill indicator works for any model the API knows about,
	// not just the curated hardcoded table. fire-and-forget; failure is
	// silent — ContextWindow falls back to the hardcoded map.
	if pc, ok := cfg.Providers[cfg.DefaultProvider]; ok {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = llm.ListModels(ctx, cfg.DefaultProvider, pc)
		}()
	}

	sessID := cmp.Or(resumeID, uuid.NewString())
	roll, err := session.Open(sessID)
	if err != nil {
		exitf(1, "bee: session: %v", err)
	}
	defer roll.Close()

	var prior []types.Message
	if resumeID != "" {
		ms, rerr := session.ReadResume(resumeID)
		if rerr != nil {
			exitf(1, "bee: read session %s: %v", resumeID, rerr)
		}
		prior = stripAgentPreambleMessages(ms)
	}
	eng := &loop.Engine{
		Provider:    prov,
		Tools:       reg,
		Skills:      skillReg,
		Waggle:      waggleManager(cfg, cwd),
		Replay:      waggleReplayer(cfg, cwd),
		Memory:      newKnowledgeAdapter(prov, cfg),
		Sessions:    roll,
		HostSession: true,
		Cfg:         cfg,
		Cwd:         cwd,
		Stdout:      os.Stdout,
		// buffered so a quick burst of Enter-steers from the TUI doesn't
		// block; loop drains one per iteration anyway.
		SteerCh: make(chan string, 4),
		Jobs:    jm,
		// stream channel routes text deltas from the engine through bubbletea
		// instead of letting them write to os.Stdout (which would corrupt the
		// alt-screen). Buffered so brief consumer hiccups don't drop deltas.
		// sized so a fast provider can outrun a busy Update cycle without the
		// engine's non-blocking send dropping deltas; the pump batch-drains.
		StreamCh: make(chan string, 512),
		// ThinkCh routes reasoning deltas from the engine through bubbletea so
		// chain-of-thought renders live above the answer instead of arriving
		// in one batch after streaming ends. Same buffer size as StreamCh — a
		// reasoning model can emit deltas just as fast as text.
		ThinkCh: make(chan string, 512),
		// ProgressCh routes withheld-output char counts from buffered tool-call
		// modes so the loader's ↓ figure moves even when no text deltas flow.
		ProgressCh: make(chan int, 512),
		// LiveMsgCh surfaces each assistant/tool message as the loop appends it,
		// so tool_use / tool_result cards render mid-Run instead of only at
		// turnDoneMsg. Buffered to avoid stalling the loop during tool bursts.
		LiveMsgCh:       make(chan types.Message, 32),
		ToolTailCh:      make(chan loop.ToolTail, 16),
		WarnCh:          make(chan string, 8),
		Costs:           cost.New(),
		InitialMessages: prior,
		VisionAsker:     herdr.WrapAsker(tuiAsker),
		PersistVision:   persistVisionModel,
	}
	// rebuild closure: invoked by the TUI after /model or the picker switches
	// provider/model so the next turn uses a freshly-constructed client (and
	// memory adapter pointed at the new selector model) instead of the one
	// captured here at start-up. also rebuilds the tool registry so mid-session
	// config changes (e.g. /browser enabling browser tools) take effect without
	// a restart. builds into a fresh registry first; only swaps on success.
	eng.Rebuild = func(e *loop.Engine) error {
		newProv, err := buildProvider(e.Cfg)
		if err != nil {
			return err
		}
		newReg, err := toolsFor(cwd, e.Cfg, newProv, withJobs(jm))
		if err != nil {
			return err
		}
		e.Provider = newProv
		e.Memory = newKnowledgeAdapter(newProv, e.Cfg)
		e.Tools = newReg
		return nil
	}

	// provider builder: the hive puts workers and reviewers on their own
	// [swarm] model tier through it.
	eng.ProviderFor = buildProvider

	// tools-for-cwd closure: queen mode calls this to root an isolated worker
	// engine at its own git worktree so file tools target the worktree instead
	// of the shared cwd. Same builder as Rebuild, just parameterized on path.
	eng.ToolsForCwd = func(wcwd string) (*tools.Registry, error) {
		return toolsFor(wcwd, eng.Cfg, eng.Provider)
	}

	// Startup intro animation is non-blocking: tui.RunWithCommandsAndKeyMap
	// wires it into the bubbletea model when cfg.ShowBanner is true, so the
	// user can already type while frames advance.
	//
	// Build the slash command registry up front so callers/plugins can
	// extend it before tui.Run kicks off. NewModel auto-seeds one too,
	// but going through RunWithCommands keeps the wiring explicit.
	cmdReg := commands.NewRegistry()
	commands.RegisterBuiltins(cmdReg)

	// load ~/.bee/keybindings.json overrides on top of defaults
	beeHome, _ := beehome.Dir()
	km := tui.LoadKeyMap(beeHome)

	// resume: if the seeded history is still over the compaction threshold,
	// summarize it once up front (cheap model, visible notice) so the first turn
	// isn't a multi-minute prompt-processing stall on a slow local model.
	if resumeID != "" && len(eng.InitialMessages) > 0 {
		fmt.Fprintf(os.Stderr, "bee: resuming %s — checking history size…\n", resumeID)
		if stats, ran, perr := eng.PrepareResume(context.Background()); perr != nil {
			fmt.Fprintf(os.Stderr, "bee: resume compaction failed (continuing with full history): %v\n", perr)
		} else if ran {
			fmt.Fprintf(os.Stderr, "bee: compacted resumed history ~%dk→~%dk tokens\n",
				stats.BeforeTokens/1000, stats.AfterTokens/1000)
		}
	}

	// show bee in the hosting herdr pane right away, not only after the first
	// turn (no-op outside herdr); os.Exit below skips the deferred release.
	sessPath, _ := session.Path(sessID)
	herdr.Ready(herdr.Session{ID: sessID, Path: sessPath})
	defer herdr.Close()

	if err := tui.RunSeededAsker(context.Background(), eng, cmdReg, km, tuiApprover, tuiAsker, seedPrompt); err != nil {
		fmt.Fprintf(os.Stderr, "bee: tui: %v\n", err)
		// os.Exit skips defers: persist the allowlist and sync the rollout first
		jm.Close()
		herdr.Close()
		if err := app.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "bee: allowlist not saved: %v\n", err)
		}
		_ = roll.Close()
		os.Exit(1)
	}
	// /new, /back, /fork, /clone swap eng.Sessions mid-run; print the active
	// id, not the one we opened with, or `bee back` resumes the wrong session.
	finalID := sessID
	if eng.Sessions != nil {
		if id := eng.Sessions.ID(); id != "" {
			finalID = id
		}
	}
	fmt.Fprintf(os.Stderr, "bee back %s\n", finalID)
}
