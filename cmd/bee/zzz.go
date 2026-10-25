// Subcommand `bee zzz`: overnight autonomous-commit loop. gnhf port —
// see internal/zzz for the inner loop. Builds an engine the same way
// runHeadless does, then hands control to zzz.Drive.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/zzz"
	zzztui "github.com/elhenro/bee/internal/zzz/tui"
)

func runZzz(args []string) {
	fs := flag.NewFlagSet("zzz", flag.ContinueOnError)
	maxIter := fs.Int("max-iterations", 0, "stop after N iterations (0 = auto: 100, or 40 for small/local models)")
	maxTok := fs.Int("max-tokens", -1, "stop after N total tokens (-1 = auto per profile, 0 = unlimited)")
	stopWhen := fs.String("stop-when", "", "stop when assistant text contains this substring")
	wantWorktree := fs.Bool("worktree", false, "run in isolated git worktree under ~/.bee/zzz/worktrees/")
	currentBranch := fs.Bool("current-branch", false, "commit to the current branch instead of zzz/<id>")
	push := fs.Bool("push", false, "git push -u origin <branch> after every commit")
	sign := fs.Bool("sign", false, "sign commits (default unsigned — overnight loops shouldn't prompt for GPG/SSH)")
	noVerify := fs.Bool("no-verify", false, "skip pre-commit hooks (opt-in)")
	resumeID := fs.String("resume", "", "resume run id (default: most-recent)")
	wantList := fs.Bool("list", false, "list runs and exit")
	cleanupID := fs.String("cleanup", "", "remove worktree for run id and exit")
	gcRuns := fs.Bool("gc", false, "prune terminal runs + done bg sessions, then exit")
	gcMaxAge := fs.Duration("gc-max-age", 14*24*time.Hour, "with --gc: delete entries older than this")
	gcKeep := fs.Int("gc-keep", 20, "with --gc: keep this many newest entries regardless of age")
	gcWorktrees := fs.Bool("gc-worktrees", false, "with --gc: also `git worktree remove` pruned worktree runs (run from the main repo)")
	gcStale := fs.Duration("gc-stale-running", 7*24*time.Hour, "with --gc: mark runs stuck in 'running' older than this as aborted (0 = never)")
	model := fs.String("model", "", "override config default_model")
	provider := fs.String("provider", "", "override config default_provider")
	sandboxScope := fs.String("sandbox", "", "override sandbox scope")
	safe := fs.Bool("safe", false, "shorthand for --sandbox workspace-write: confine writes to cwd+tmp, block network")
	thinking := fs.String("thinking", "", "thinking level: auto|off|low|medium|high|max")
	effort := fs.String("effort", "", "alias for --thinking")
	cavemanLvl := fs.String("caveman", "", "force caveman level")
	yes := fs.Bool("yes", false, "auto-approve dangerous shell commands")
	yolo := fs.Bool("yolo", false, "alias for --yes")
	plain := fs.Bool("plain", false, "force single-line status renderer (no full-screen TUI)")
	maxFails := fs.Int("max-consecutive-fails", 0, "end run after N consecutive failed iters (0=default 3)")
	retries := fs.Int("hard-error-retries", 0, "engine.Run retries per iter on transient errors (0=default 3)")
	notesTail := fs.Int("notes-tail", 0, "include only last N prior-iter sections in prompts (0=default 5, <0=unlimited)")
	noGate := fs.Bool("no-gate", false, "commit without running the verify gate (.bee/verify or [swarm] verify)")
	agents := fs.Int("agents", 1, "queen mode: run N headless worker bees in parallel worktrees on the same objective")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	if *wantList {
		if err := zzzList(); err != nil {
			exitf(1, "zzz: list: %v", err)
		}
		return
	}
	if *cleanupID != "" {
		if err := zzzCleanup(*cleanupID); err != nil {
			exitf(1, "zzz: cleanup: %v", err)
		}
		return
	}
	if *gcRuns {
		runGC(*gcMaxAge, *gcKeep, *gcWorktrees, *gcStale)
		return
	}

	autoYes := *yes || *yolo
	objective := strings.Join(fs.Args(), " ")
	isResume := *resumeID != "" || (objective == "" && !hasStdinPipe())
	if objective == "" && !isResume {
		objective = readStdinOrEmpty()
		if objective == "" {
			exitf(1, "zzz: no objective given (positional arg, stdin pipe, or --resume)")
		}
	}

	// zzz runs unattended, so it confines to the workspace by default (--safe).
	// An explicit --sandbox/--safe still wins; --sandbox danger-full-access opts
	// back into unrestricted access.
	cfg, prov, app, skillReg, err := buildZzzDeps(*model, *provider, resolveSafe(*sandboxScope, *safe, "workspace-write"), *thinking, *effort, *cavemanLvl, autoYes)
	if err != nil {
		exitf(1, "zzz: setup: %v", err)
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	stopCh := make(chan struct{})
	go installShutdownHandler(stopCh, cancelCtx)

	// resolve auto budgets by profile: small/local models get tighter caps so a
	// confused model can't spin a whole night burning tokens. explicit flags win.
	resolvedMaxIter, resolvedMaxTok := resolveZzzBudgets(cfg, *maxIter, *maxTok)

	zCfg := zzz.Config{
		Objective:           objective,
		MaxIterations:       resolvedMaxIter,
		MaxTokens:           resolvedMaxTok,
		StopWhen:            *stopWhen,
		Worktree:            *wantWorktree,
		CurrentBranch:       *currentBranch,
		Push:                *push,
		Sign:                *sign,
		NoVerify:            *noVerify,
		MaxConsecutiveFails: *maxFails,
		HardErrorRetries:    *retries,
		NotesTailIters:      *notesTail,
		VerifyTimeout:       cfg.Swarm.VerifyTimeout(),
		MaxHops:             cfg.Swarm.Hops(),
		SandboxScope:        cfg.Sandbox.Scope,
	}
	if !*noGate {
		zCfg.Verify = zzzGate(cfg)
	}

	if autoYes && !*wantWorktree && !isResume {
		fmt.Fprintln(os.Stderr, "[zzz] WARNING: --yes/--yolo without --worktree lets the agent run dangerous shell commands (rm -rf ., git reset --hard, …) directly on your working branch.")
	}
	if resolvedMaxTok == 0 {
		fmt.Fprintln(os.Stderr, "[zzz] WARNING: no token cap (--max-tokens 0) — an unproductive model can burn unbounded tokens overnight.")
	}

	if *agents > 1 {
		if isResume {
			exitf(1, "zzz: --agents and --resume can't be combined")
		}
		if !autoYes {
			exitf(1, "zzz: --agents needs --yes (headless workers can't answer approval prompts)")
		}
		runZzzQueen(ctx, cancelCtx, *agents, cfg, prov, app, skillReg, zCfg)
		return
	}

	// start and resume take the tree's lock before touching the tree or the
	// run's meta, so a second zzz on the same tree changes nothing.
	var run *zzz.Run
	var unlock func()
	if isResume {
		run, unlock, err = resumeRun(*resumeID)
	} else {
		run, unlock, err = startRun(zCfg)
	}
	if err != nil {
		exitf(1, "zzz: %v", err)
	}
	defer unlock()

	// resume carries its objective in meta.json (prompt.txt as fallback); the
	// flag-derived zCfg.Objective is empty on resume, so restore it here or the
	// resumed agent runs goal-less.
	if isResume {
		zCfg.Objective = run.Objective
		if strings.TrimSpace(zCfg.Objective) == "" {
			if p, perr := zzz.LoadPrompt(run.ID); perr == nil {
				zCfg.Objective = strings.TrimSpace(p)
			}
		}
		if strings.TrimSpace(zCfg.Objective) == "" {
			exitf(1, "zzz: resume %s has no saved objective; re-run with the objective text", run.ID)
		}
	}

	useTUI := !*plain && term.IsTerminal(int(os.Stdout.Fd()))
	var engineOut io.Writer = os.Stdout
	var tuiModel *zzztui.Model
	if useTUI {
		tuiModel = zzztui.New(run, zCfg)
		engineOut = tuiModel.EngineWriter()
	}

	eng, cleanup, err := buildZzzEngine(cfg, prov, app, skillReg, run.RepoRoot, engineOut)
	if err != nil {
		exitf(1, "zzz: engine: %v", err)
	}
	defer cleanup()
	if c, ok := app.(*approval.Cache); ok {
		defer c.Flush()
	}

	runner := zzz.NewLoopRunner(eng)
	if useTUI {
		runZzzTUI(ctx, stopCh, runner, zCfg, run, tuiModel)
	} else if err := driveZzzPlain(ctx, stopCh, runner, zCfg, run); err != nil {
		exitf(1, "zzz: drive: %v", err)
	}
	fmt.Fprintf(os.Stderr, "\n→ inspect: ~/.bee/zzz/runs/%s/  (notes.md, events.jsonl, meta.json)\n", run.ID)
}

// driveZzzPlain runs the loop with the single-line status renderer.
func driveZzzPlain(ctx context.Context, stopCh chan struct{}, runner zzz.Runner, zCfg zzz.Config, run *zzz.Run) error {
	status := zzz.NewStatus(os.Stderr)
	var ui zzz.UI = status
	// only attach a stdin reader when stdin is a tty: piped stdin already
	// served as objective input and is closed/empty by now.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		ps := &plainSteerUI{Status: status, steer: make(chan zzz.Steer, 16)}
		go readStdinSteer(ctx, ps)
		ui = ps
		fmt.Fprintln(os.Stderr, "[zzz] plain mode: type /stop, /abort, /note <text>, or free text to nudge.")
	}
	return zzz.Drive(ctx, stopCh, runner, zCfg, run, ui)
}

// plainSteerUI gives plain-mode (non-TUI) operators a steering surface by
// satisfying zzz.Steerable on top of the line-status renderer.
type plainSteerUI struct {
	*zzz.Status
	steer chan zzz.Steer
}

func (p *plainSteerUI) Steer() <-chan zzz.Steer { return p.steer }

func readStdinSteer(ctx context.Context, p *plainSteerUI) {
	r := bufio.NewScanner(os.Stdin)
	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if line == "" {
			continue
		}
		select {
		case p.steer <- parseSteerLine(line):
		case <-ctx.Done():
			return
		}
	}
}

func parseSteerLine(s string) zzz.Steer {
	if !strings.HasPrefix(s, "/") {
		return zzz.Steer{Kind: zzz.SteerNote, Text: s}
	}
	cmd, rest, _ := strings.Cut(s, " ")
	switch strings.ToLower(cmd) {
	case "/stop", "/quit":
		return zzz.Steer{Kind: zzz.SteerStop}
	case "/abort", "/kill":
		return zzz.Steer{Kind: zzz.SteerAbort}
	case "/note", "/say", "/nudge":
		return zzz.Steer{Kind: zzz.SteerNote, Text: strings.TrimSpace(rest)}
	}
	return zzz.Steer{Kind: zzz.SteerNote, Text: s}
}

// runZzzTUI launches Drive in a goroutine and blocks on the bubbletea
// program in this goroutine. When Drive returns, the TUI flips to its
// summary panel and waits for q/ctrl+d. If the operator quits first,
// ctx-cancel propagates a hard stop to Drive.
func runZzzTUI(parentCtx context.Context, stopCh chan struct{}, runner zzz.Runner, zCfg zzz.Config, run *zzz.Run, model *zzztui.Model) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	done := make(chan struct{})
	var driveErr error
	go func() {
		driveErr = zzz.Drive(ctx, stopCh, runner, zCfg, run, model)
		model.Done(run, driveErr)
		close(done)
	}()
	if err := model.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zzz tui: %v\n", err)
	}
	cancel()
	// silence further Model.send() calls from Drive's goroutine before it
	// observes the canceled context, otherwise post-Run sends race with TUI
	// teardown.
	model.Quit()
	<-done
	// altscreen restores on quit — re-render the summary to stderr so the
	// operator sees the outcome after the TUI goes away.
	zzz.NewStatus(os.Stderr).RenderSummary(run)
	if driveErr != nil {
		fmt.Fprintf(os.Stderr, "zzz: drive: %v\n", driveErr)
	}
}

// installShutdownHandler unifies SIGINT and SIGTERM behavior:
//   - first signal → close stopCh so Drive exits gracefully after the current
//     iteration. Run status ends as "aborted" with a graceful cause.
//   - second signal → cancel the context so Drive's engine call returns
//     immediately. Same observed exit shape regardless of which signal arrived
//     first.
//
// This avoids the previous split where SIGINT and SIGTERM produced different
// exit semantics (one graceful, one ctx-cancel).
func installShutdownHandler(stopCh chan struct{}, cancelCtx context.CancelFunc) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	close(stopCh)
	fmt.Fprintln(os.Stderr, "\n[zzz] shutdown signal — will exit after current iteration. Send again to force.")
	<-c
	fmt.Fprintln(os.Stderr, "[zzz] second signal — cancelling engine mid-iteration.")
	cancelCtx()
}

func hasStdinPipe() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) == 0
}

func readStdinOrEmpty() string {
	if !hasStdinPipe() {
		return ""
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
