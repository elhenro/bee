// Interactive TUI entry point. Builds the same Engine as `bee run` but
// hands it to the bubbletea program in internal/tui.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/muesli/termenv"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tui"
	"github.com/elhenro/bee/internal/types"
)

// runTUI wires the full engine the same way runHeadlessReal does, then
// hands control to internal/tui. Returns once the program exits.
func runTUI() { runTUIWithSession("") }

// detectDarkBg picks lipgloss bg mode.
//   - BEE_THEME=light|dark forces it
//   - else COLORFGBG=<fg>;<bg> — bg<7 = dark (xterm-ish convention)
//   - else OSC 11 query via termenv (runs BEFORE bubbletea grabs stdin
//     so the terminal reply is consumed cleanly)
//   - else default dark
func detectDarkBg() bool {
	if t := strings.ToLower(strings.TrimSpace(os.Getenv("BEE_THEME"))); t != "" {
		return t != "light"
	}
	if v := os.Getenv("COLORFGBG"); v != "" {
		parts := strings.Split(v, ";")
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return n < 7
		}
	}
	// termenv.HasDarkBackground sends OSC 11 and reads the reply. Safe here
	// because we're called before tea.NewProgram puts stdin in raw mode.
	// Falls back to true on any error (no TTY, query timeout, etc.).
	return termenv.HasDarkBackground()
}

// runTUIWithSession is runTUI plus an optional pre-existing session id.
// When non-empty, the rollout is reopened in append mode and prior messages
// are seeded into the TUI / engine so the conversation continues.
func runTUIWithSession(resumeID string) {
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

	cfg, err := config.Load()
	if err != nil {
		if os.Getenv("BEE_TEST_PROVIDER") == "stub" {
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "bee: config: %v\n", err)
			os.Exit(1)
		}
	}

	prov, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee: provider: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	reg, err := buildTools(cwd, cfg, prov, storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee: tools: %v\n", err)
		os.Exit(1)
	}

	ensureFirstRun()
	skillReg := skills.NewRegistry()
	_ = skillReg.Load(skills.BaseDir())

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

	sessID := resumeID
	if sessID == "" {
		sessID = uuid.NewString()
	}
	roll, err := session.Open(sessID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee: session: %v\n", err)
		os.Exit(1)
	}
	defer roll.Close()

	var prior []types.Message
	if resumeID != "" {
		ms, rerr := session.Read(resumeID)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "bee: read session %s: %v\n", resumeID, rerr)
			os.Exit(1)
		}
		prior = ms
	}
	// stream channel routes text deltas from the engine through bubbletea
	// instead of letting them write to os.Stdout (which would corrupt the
	// alt-screen). Buffered so brief consumer hiccups don't drop deltas.
	streamCh := make(chan string, 64)
	// liveMsgCh surfaces each assistant/tool message as the loop appends it,
	// so tool_use / tool_result cards render mid-Run instead of only at
	// turnDoneMsg. Buffered to avoid stalling the loop during tool bursts.
	liveMsgCh := make(chan types.Message, 32)
	warnCh := make(chan string, 8)
	costs := cost.New()
	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Memory:   newKnowledgeAdapter(prov, cfg),
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   os.Stdout,
		// buffered so a quick burst of Enter-steers from the TUI doesn't
		// block; loop drains one per iteration anyway.
		SteerCh:         make(chan string, 4),
		StreamCh:        streamCh,
		LiveMsgCh:       liveMsgCh,
		WarnCh:          warnCh,
		Costs:           costs,
		InitialMessages: prior,
	}
	// rebuild closure: invoked by the TUI after /model or the picker switches
	// provider/model so the next turn uses a freshly-constructed client (and
	// memory adapter pointed at the new selector model) instead of the one
	// captured here at start-up.
	eng.Rebuild = func(e *loop.Engine) error {
		newProv, err := buildProvider(e.Cfg)
		if err != nil {
			return err
		}
		e.Provider = newProv
		e.Memory = newKnowledgeAdapter(newProv, e.Cfg)
		return nil
	}

	// print startup banner (config-gated). BEE_BANNER picks variant —
	// "hex" / "swarm" / "comb" / "flower" / "random" (default = random).
	if cfg.ShowBanner {
		bv := tui.ParseBannerVariant(os.Getenv("BEE_BANNER"))
		fmt.Fprint(os.Stderr, tui.RenderBannerVariant(bv, version, cfg.DefaultModel))
	}

	// Build the slash command registry up front so callers/plugins can
	// extend it before tui.Run kicks off. NewModel auto-seeds one too,
	// but going through RunWithCommands keeps the wiring explicit.
	cmdReg := commands.NewRegistry()
	commands.RegisterBuiltins(cmdReg)

	// load ~/.bee/keybindings.json overrides on top of defaults
	beeHome := os.Getenv("BEE_HOME")
	if beeHome == "" {
		if h, err := os.UserHomeDir(); err == nil {
			beeHome = filepath.Join(h, ".bee")
		}
	}
	km := tui.LoadKeyMap(beeHome)

	if err := tui.RunWithCommandsAndKeyMap(context.Background(), eng, cmdReg, km); err != nil {
		fmt.Fprintf(os.Stderr, "bee: tui: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
}
