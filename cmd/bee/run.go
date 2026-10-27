// Headless run path: parse flags, build engine, drive one turn, exit.
//
// Skill shims and CI smoke all exec through here. Pipes user message in
// from args or stdin; streams assistant text to stdout; prints session id
// to stderr on exit so callers can resume or inspect.
package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/herdr"
	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/skills/bundled"
)

// runHeadless is the headless implementation behind `bee run`. Cleanup is
// deferred inside headlessRun, so a non-zero code exits only after it ran.
func runHeadless(args []string) {
	if code := headlessRun(args); code != 0 {
		os.Exit(code)
	}
}

func headlessRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.Bool("headless", true, "run without TUI (default true)")
	var ef engineFlags
	ef.register(fs)
	ef.registerAutoApprove(fs)
	skillName := fs.String("skill", "", "run a skill as the user message (prompt-kind only in Wave 2)")
	jsonOut := fs.Bool("json", false, "emit NDJSON events to stdout instead of text")
	allowedTools := fs.String("allowed-tools", "", "comma-list of tool names to expose (default: all)")
	sessionID := fs.String("session", "", "use this session id instead of a fresh uuid")
	writeFilter := fs.String("write-path-re", "", "regex; writes restricted to paths matching this regex (default: no filter)")
	verbose := fs.Bool("verbose", false, "show full tool output (default: compact one-line preview)")
	bgLoop := fs.Bool("bg-loop", false, "persist after first turn: write status sidecar, poll inbox for follow-ups")
	browserOn := fs.Bool("browser", false, "enable native browser tools (open/snapshot/click/type/console) for this run")
	parseFlags(fs, args)
	if *verbose {
		_ = os.Setenv("BEE_VERBOSE", "1")
	}

	var writeRe *regexp.Regexp
	if *writeFilter != "" {
		re, err := regexp.Compile(*writeFilter)
		if err != nil {
			exitf(2, "bee run: bad --write-path-re: %v", err)
		}
		writeRe = re
	}

	userMsg := strings.Join(fs.Args(), " ")
	// a skill body drives the turn; stdin would block on tty.
	if len(fs.Args()) == 0 && *skillName == "" {
		var err error
		if userMsg, err = resolveUserMessage(nil, os.Stdin); err != nil {
			exitf(2, "bee run: %v", err)
		}
	}

	cfg := ef.mustLoad("bee run", "")
	// scripted provider needs deterministic Stream-call count: pin worker role
	// (no per-turn classifier side-query fires — classifier was removed) and
	// disable compaction + memory selection. tests script exactly the calls
	// they expect.
	if os.Getenv("BEE_TEST_PROVIDER") == "scripted" {
		cfg.Role = "worker"
		cfg.Compaction.Enabled = false
		cfg.Memory.Enabled = false
	}
	if *browserOn {
		cfg.Browser.Enabled = true
	}

	prov := mustProvider("bee run", cfg)

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	app := buildHeadlessApprover(cfg, ef.autoApprove())
	if c, ok := app.(*approval.Cache); ok {
		defer c.Flush()
	}
	// background commands, only for a run shape that waits for them (see awaitJobs)
	jm := headlessJobs(*bgLoop, *skillName, userMsg)
	defer jm.Close()
	reg, err := buildToolsWithApprover(cwd, cfg, prov, storeDir, app, withJobs(jm), withWriteFilter(writeRe))
	if err != nil {
		exitf(1, "bee run: tools: %v", err)
	}
	ensureFirstRun()
	skillReg := newSkillRegistry()
	registerSkillTools(reg, skillReg, cfg, cwd)

	if *allowedTools != "" {
		if reg, err = filterTools(reg, *allowedTools); err != nil {
			exitf(2, "bee run: %v", err)
		}
	}

	var recipeSkill *skills.Skill
	if *skillName != "" {
		recipeSkill, userMsg = resolveRunSkill(skillReg, *skillName, userMsg)
	}

	sessID := cmp.Or(*sessionID, uuid.NewString())
	roll, err := session.Open(sessID)
	if err != nil {
		exitf(1, "bee run: session: %v", err)
	}
	defer roll.Close()
	defer herdr.Close()

	// the queen hive only runs in the TUI; headless falls back to a single
	// worker turn rather than locking up. note it so the behavior isn't silent.
	if loop.ParseRole(cfg.Role) == loop.RoleQueen {
		fmt.Fprintln(os.Stderr, "bee: queen role spawns a hive only in the TUI — running a single worker turn here")
	}

	eng := newHeadlessEngine(prov, reg, skillReg, cfg, cwd, roll, os.Stdout)
	eng.Waggle = waggleManager(cfg, cwd)
	eng.Replay = waggleReplayer(cfg, cwd)
	eng.HostSession = true
	eng.Jobs = jm
	if *jsonOut {
		eng.JSONEmitter = jsonmode.New(os.Stdout)
		// text deltas already routed through emitter; suppress raw writes.
		eng.Stdout = io.Discard
	}

	if recipeSkill != nil {
		return runRecipeHeadless(eng, *recipeSkill, fs.Args())
	}

	// signal parent so Ctrl-C surfaces as a clean context.Canceled (the
	// watchdog never resumes a user abort) rather than killing the process.
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *bgLoop {
		// long-lived bg path: signal-cancellable ctx, no timeout.
		if err := runBgLoop(parent, eng, sessID, userMsg); err != nil {
			fmt.Fprintf(os.Stderr, "bee run: bg-loop: %v\n", err)
			return 1
		}
		return 0
	}

	// headless /goal: a "/goal <condition>" message drives an auto-loop instead
	// of a single turn. Bare/clear/show have no persistent state to act on, so
	// they print a hint and exit cleanly.
	if cond, isGoal := parseGoalMessage(userMsg); isGoal {
		if cond == "" {
			fmt.Fprintln(os.Stderr, "bee: /goal needs a condition in headless mode")
			return 0
		}
		return runGoalHeadless(parent, eng, cfg, sessID, cond)
	}

	res, err := runWithResume(parent, eng, userMsg)
	// background jobs outlive the turn that started them: keep going until they
	// have all reported back and the model has had its say on each
	if err == nil && eng.Jobs != nil {
		err = awaitJobs(parent, eng, res, *jsonOut)
	}

	if err != nil {
		if *jsonOut {
			eng.JSONEmitter.Emit(jsonmode.Event{Type: "error", Message: err.Error()})
		} else {
			fmt.Fprintf(os.Stderr, "\nbee run: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
		return exitCodeForErr(err)
	}
	if !*jsonOut {
		// trailing newline so prompts don't merge with assistant output
		fmt.Fprintln(os.Stdout)
	}
	fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
	return 0
}

// newSkillRegistry loads the installed skills. Valid ones load even when
// others fail to parse; those are reported so a malformed skill does not just
// vanish.
func newSkillRegistry() *skills.Registry {
	reg := skills.NewRegistry()
	if err := reg.Load(skills.BaseDir()); err != nil {
		fmt.Fprintf(os.Stderr, "bee: %v\n", err)
	}
	return reg
}

// resolveRunSkill looks up the --skill name (registry, then waggle stores). A
// runtime recipe is returned for execution in Go once the engine exists, and
// the positional args feed its templates rather than a user message. A
// prompt-kind skill, or a legacy recipe (recipeBuild folds its steps into
// Body), is prepended to the user message. Exec-kind skills are registered as
// callable tools by the caller. An unknown name exits 2.
func resolveRunSkill(reg *skills.Registry, name, userMsg string) (*skills.Skill, string) {
	s, ok := reg.Get(name)
	if !ok {
		s, ok = waggleSkill(name)
	}
	if !ok {
		exitf(2, "bee run: unknown skill %q", name)
	}
	switch {
	case s.Kind == skills.KindRecipe && s.Runtime:
		return &s, userMsg
	case (s.Kind == skills.KindPrompt || s.Kind == skills.KindRecipe) && s.Body != "":
		if userMsg == "" {
			return nil, s.Body
		}
		return nil, s.Body + "\n\nInput: " + userMsg
	}
	return nil, userMsg
}

// ensureFirstRun is best-effort. Called from `bee run` and TUI startup.
// Silent on success, never fatal: a broken HOME shouldn't block the agent.
// WriteDefaults is idempotent and preserves user edits (existing files are
// never overwritten), so calling it every start back-fills any bundled
// skills added since the user's last seed.
func ensureFirstRun() {
	_, _ = bundled.WriteDefaults(skills.BaseDir())
}
