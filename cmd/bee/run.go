// Headless run path: parse flags, build engine, drive one turn, exit.
//
// Skill shims and CI smoke all exec through here. Pipes user message in
// from args or stdin; streams assistant text to stdout; prints session id
// to stderr on exit so callers can resume or inspect.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
)

// runHeadlessReal is the actual headless implementation. main.go calls
// it via the runHeadless alias.
func runHeadlessReal(args []string) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	headless := fs.Bool("headless", true, "run without TUI (default true)")
	model := fs.String("model", "", "override config default_model")
	providerName := fs.String("provider", "", "override config default_provider")
	sandboxScope := fs.String("sandbox", "", "override sandbox scope (read-only|workspace-write|danger-full-access)")
	skillName := fs.String("skill", "", "run a skill as the user message (prompt-kind only in Wave 2)")
	thinking := fs.String("thinking", "", "thinking level: auto|off|low|medium|high|max (default: from config)")
	effort := fs.String("effort", "", "alias for --thinking: auto|off|low|medium|high|max")
	cavemanLvl := fs.String("caveman", "", "force caveman level: off|lite|full|ultra (overrides profile, even on tiny)")
	jsonOut := fs.Bool("json", false, "emit NDJSON events to stdout instead of text")
	allowedTools := fs.String("allowed-tools", "", "comma-list of tool names to expose (default: all)")
	sessionID := fs.String("session", "", "use this session id instead of a fresh uuid")
	writeFilter := fs.String("write-path-re", "", "regex; writes restricted to paths matching this regex (default: no filter)")
	extraTools := fs.String("extra-tools", "", "comma-list of expert-mode tools to add to the manifest (e.g. apply_patch,hashline_edit). default: off")
	verbose := fs.Bool("verbose", false, "show full tool output (default: compact one-line preview)")
	bgLoop := fs.Bool("bg-loop", false, "persist after first turn: write status sidecar, poll inbox for follow-ups")
	yes := fs.Bool("yes", false, "auto-approve any dangerous shell command without prompting (still blocks hardline-refused commands)")
	yolo := fs.Bool("yolo", false, "alias for --yes: auto-approve dangerous commands")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	_ = headless // single-path for now
	if *verbose {
		_ = os.Setenv("BEE_VERBOSE", "1")
	}

	var writeRe *regexp.Regexp
	if *writeFilter != "" {
		re, err := regexp.Compile(*writeFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee run: bad --write-path-re: %v\n", err)
			os.Exit(2)
		}
		writeRe = re
	}

	var userMsg string
	if len(fs.Args()) > 0 {
		userMsg = strings.Join(fs.Args(), " ")
	} else if *skillName != "" {
		// skill body drives the turn; stdin would block on tty.
		userMsg = ""
	} else {
		var err error
		userMsg, err = resolveUserMessage(nil, os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee run: %v\n", err)
			os.Exit(2)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		// in test mode we synthesize a stub config so missing API keys
		// don't break the smoke test.
		if os.Getenv("BEE_TEST_PROVIDER") != "" {
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "bee run: config: %v\n", err)
			os.Exit(1)
		}
	}
	// scripted provider needs deterministic Stream-call count: disable the
	// classifier (mode=auto fires a side-query), compaction, and memory
	// selection. tests script exactly the calls they expect.
	if os.Getenv("BEE_TEST_PROVIDER") == "scripted" {
		cfg.Mode = "edit"
		cfg.Compaction.Enabled = false
		cfg.Memory.Enabled = false
	}
	applyOverrides(&cfg, *model, *providerName, *sandboxScope)
	// --effort is an alias for --thinking. Global `bee --effort` (stripped
	// in main.go) lands in BEE_EFFORT and is consumed here as the lowest-
	// priority source; explicit subcommand flags override env.
	effortVal := *effort
	if effortVal == "" {
		effortVal = os.Getenv("BEE_EFFORT")
	}
	if *thinking == "" && effortVal != "" {
		*thinking = effortVal
	}
	if *thinking != "" {
		cfg.Thinking = string(llm.ParseThinking(*thinking))
	}
	cavemanArg := *cavemanLvl
	if cavemanArg == "" {
		cavemanArg = os.Getenv("BEE_CAVEMAN") // global --caveman flag
	}
	if cavemanArg != "" {
		// validate so a typo fails fast instead of silently falling back to
		// profile default in ApplyProfile.
		lvl, err := caveman.ParseLevel(cavemanArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee run: %v\n", err)
			os.Exit(2)
		}
		cfg.Caveman = string(lvl)
	}
	if *extraTools != "" {
		for _, p := range strings.Split(*extraTools, ",") {
			if p = strings.TrimSpace(p); p != "" {
				cfg.ExtraTools = append(cfg.ExtraTools, p)
			}
		}
	}

	prov, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee run: provider: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	app := buildHeadlessApprover(cfg, *yes || *yolo)
	if c, ok := app.(*approval.Cache); ok {
		defer c.Flush()
	}
	var reg *tools.Registry
	if writeRe != nil {
		reg, err = buildToolsFilteredWithApprover(cwd, cfg, writeRe, prov, storeDir, app)
	} else {
		reg, err = buildToolsWithApprover(cwd, cfg, prov, storeDir, app)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee run: tools: %v\n", err)
		os.Exit(1)
	}
	if *allowedTools != "" {
		if reg, err = filterTools(reg, *allowedTools); err != nil {
			fmt.Fprintf(os.Stderr, "bee run: %v\n", err)
			os.Exit(2)
		}
	}

	ensureFirstRun()
	skillReg := skills.NewRegistry()
	_ = skillReg.Load(skills.BaseDir()) // best-effort

	if *skillName != "" {
		s, ok := skillReg.Get(*skillName)
		if !ok {
			fmt.Fprintf(os.Stderr, "bee run: unknown skill %q\n", *skillName)
			os.Exit(2)
		}
		// prompt-kind: prepend body to user message. exec/mcp/http defer to v0.2.
		if s.Kind == skills.KindPrompt && s.Body != "" {
			if userMsg == "" {
				userMsg = s.Body
			} else {
				userMsg = s.Body + "\n\nUser: " + userMsg
			}
		}
	}

	sessID := *sessionID
	if sessID == "" {
		sessID = uuid.NewString()
	}
	roll, err := session.Open(sessID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee run: session: %v\n", err)
		os.Exit(1)
	}
	defer roll.Close()

	memStore := newKnowledgeAdapter(prov, cfg)

	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Memory:   memStore,
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   os.Stdout,
		Costs:    cost.New(),
	}
	if *jsonOut {
		eng.JSONEmitter = jsonmode.New(os.Stdout)
		// text deltas already routed through emitter; suppress raw writes.
		eng.Stdout = io.Discard
	}

	if *bgLoop {
		// long-lived bg path: signal-cancellable ctx, no timeout.
		bgCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runBgLoop(bgCtx, eng, sessID, userMsg); err != nil {
			fmt.Fprintf(os.Stderr, "bee run: bg-loop: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	res, err := eng.Run(ctx, userMsg)
	if err != nil {
		if *jsonOut {
			eng.JSONEmitter.Emit(jsonmode.Event{Type: "error", Message: err.Error()})
		} else {
			fmt.Fprintf(os.Stderr, "\nbee run: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
		// two-strike → distinct exit code so wrappers (zzz, swarm, CI) can
		// tell "the model wedged on a repeat" apart from generic failures.
		if errors.Is(err, loop.ErrTwoStrike) {
			os.Exit(7)
		}
		// escalate → another distinct code so the user/CI knows the model
		// asked for help rather than crashed.
		if errors.Is(err, loop.ErrEscalate) {
			os.Exit(8)
		}
		os.Exit(1)
	}
	_ = res
	if !*jsonOut {
		// trailing newline so prompts don't merge with assistant output
		fmt.Fprintln(os.Stdout)
	}
	fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
}
