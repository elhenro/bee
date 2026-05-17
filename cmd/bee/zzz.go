// Subcommand `bee zzz`: overnight autonomous-commit loop. gnhf port —
// see internal/zzz for the inner loop. Builds an engine the same way
// runHeadlessReal does, then hands control to zzz.Drive.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/term"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/zzz"
	zzztui "github.com/elhenro/bee/internal/zzz/tui"
)

func runZzz(args []string) {
	fs := flag.NewFlagSet("zzz", flag.ContinueOnError)
	maxIter := fs.Int("max-iterations", 50, "stop after N iterations (default 50)")
	maxTok := fs.Int("max-tokens", 0, "stop after N total tokens (0 = unlimited)")
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
	model := fs.String("model", "", "override config default_model")
	provider := fs.String("provider", "", "override config default_provider")
	sandboxScope := fs.String("sandbox", "", "override sandbox scope")
	thinking := fs.String("thinking", "", "thinking level: auto|off|low|medium|high|max")
	effort := fs.String("effort", "", "alias for --thinking")
	cavemanLvl := fs.String("caveman", "", "force caveman level")
	yes := fs.Bool("yes", false, "auto-approve dangerous shell commands")
	yolo := fs.Bool("yolo", false, "alias for --yes")
	plain := fs.Bool("plain", false, "force single-line status renderer (no full-screen TUI)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	if *wantList {
		if err := zzzList(); err != nil {
			fatalf("zzz: list: %v", err)
		}
		return
	}
	if *cleanupID != "" {
		if err := zzzCleanup(*cleanupID); err != nil {
			fatalf("zzz: cleanup: %v", err)
		}
		return
	}
	if *gcRuns {
		runGC(*gcMaxAge, *gcKeep)
		return
	}

	objective := strings.Join(fs.Args(), " ")
	isResume := *resumeID != "" || (objective == "" && !hasStdinPipe())
	if objective == "" && !isResume {
		objective = readStdinOrEmpty()
		if objective == "" {
			fatalf("zzz: no objective given (positional arg, stdin pipe, or --resume)")
		}
	}

	cfg, prov, app, skillReg, err := buildZzzDeps(*model, *provider, *sandboxScope, *thinking, *effort, *cavemanLvl, *yes || *yolo)
	if err != nil {
		fatalf("zzz: setup: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	stopCh := make(chan struct{})
	go installSigintHandler(stopCh)

	zCfg := zzz.Config{
		Objective:     objective,
		MaxIterations: *maxIter,
		MaxTokens:     *maxTok,
		StopWhen:      *stopWhen,
		Worktree:      *wantWorktree,
		CurrentBranch: *currentBranch,
		Push:          *push,
		Sign:          *sign,
		NoVerify:      *noVerify,
	}

	var run *zzz.Run
	if isResume {
		run, err = resumeRun(*resumeID)
	} else {
		run, err = startRun(zCfg)
	}
	if err != nil {
		fatalf("zzz: %v", err)
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
		fatalf("zzz: engine: %v", err)
	}
	defer cleanup()

	if useTUI {
		runZzzTUI(ctx, stopCh, eng, zCfg, run, tuiModel)
		fmt.Fprintf(os.Stderr, "\n→ inspect: ~/.bee/zzz/runs/%s/  (notes.md, events.jsonl, meta.json)\n", run.ID)
		return
	}
	ui := zzz.NewStatus(os.Stderr)
	if err := zzz.Drive(ctx, stopCh, eng, zCfg, run, ui); err != nil {
		fatalf("zzz: drive: %v", err)
	}
	fmt.Fprintf(os.Stderr, "\n→ inspect: ~/.bee/zzz/runs/%s/  (notes.md, events.jsonl, meta.json)\n", run.ID)
}

// runZzzTUI launches Drive in a goroutine and blocks on the bubbletea
// program in this goroutine. When Drive returns, the TUI flips to its
// summary panel and waits for q/ctrl+d. If the operator quits first,
// ctx-cancel propagates a hard stop to Drive.
func runZzzTUI(parentCtx context.Context, stopCh chan struct{}, eng *loop.Engine, zCfg zzz.Config, run *zzz.Run, model *zzztui.Model) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	done := make(chan struct{})
	var driveErr error
	go func() {
		driveErr = zzz.Drive(ctx, stopCh, eng, zCfg, run, model)
		model.Done(run, driveErr)
		close(done)
	}()
	if err := model.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zzz tui: %v\n", err)
	}
	cancel()
	<-done
	// altscreen restores on quit — re-render the summary to stderr so the
	// operator sees the outcome after the TUI goes away.
	zzz.NewStatus(os.Stderr).RenderSummary(run)
	if driveErr != nil {
		fmt.Fprintf(os.Stderr, "zzz: drive: %v\n", driveErr)
	}
}

// startRun creates a brand-new run: new id, branch (or worktree), persisted
// meta + prompt. Validates the repo root and clean tree up front.
func startRun(cfg zzz.Config) (*zzz.Run, error) {
	cwd, _ := os.Getwd()
	root, err := zzz.RepoRoot(cwd)
	if err != nil {
		return nil, fmt.Errorf("not inside a git repo: %w", err)
	}
	clean, err := zzz.IsClean(root)
	if err != nil {
		return nil, err
	}
	if !clean {
		return nil, fmt.Errorf("working tree is dirty — commit, stash, or discard before starting")
	}
	id := zzz.NewID()
	r := &zzz.Run{
		ID:        id,
		Objective: cfg.Objective,
		RepoRoot:  root,
		StartedAt: time.Now().UTC(),
		Status:    zzz.StatusRunning,
	}
	switch {
	case cfg.Worktree:
		wt, err := zzz.WorktreeDir(id)
		if err != nil {
			return nil, err
		}
		branch := "zzz/" + id
		if err := zzz.WorktreeAdd(root, wt, branch); err != nil {
			return nil, fmt.Errorf("worktree add: %w", err)
		}
		r.Mode = zzz.ModeWorktree
		r.Worktree = wt
		r.Branch = branch
		r.RepoRoot = wt
	case cfg.CurrentBranch:
		cur, err := zzz.CurrentBranch(root)
		if err != nil {
			return nil, err
		}
		r.Mode = zzz.ModeCurrent
		r.Branch = cur
	default:
		branch := "zzz/" + id
		if err := zzz.CreateBranchAndSwitch(root, branch); err != nil {
			return nil, fmt.Errorf("branch: %w", err)
		}
		r.Mode = zzz.ModeBranch
		r.Branch = branch
	}
	if err := zzz.SavePrompt(id, cfg.Objective); err != nil {
		return nil, err
	}
	if err := zzz.SaveMeta(r); err != nil {
		return nil, err
	}
	return r, nil
}

// resumeRun loads existing run meta. id="" picks the most-recent run.
func resumeRun(id string) (*zzz.Run, error) {
	var r *zzz.Run
	var err error
	if id == "" {
		r, err = zzz.LatestRun()
		if err != nil {
			return nil, err
		}
		if r == nil {
			return nil, fmt.Errorf("no prior runs to resume")
		}
	} else {
		r, err = zzz.LoadMeta(id)
		if err != nil {
			return nil, err
		}
	}
	if r.Status != zzz.StatusRunning && r.Status != zzz.StatusAborted {
		return nil, fmt.Errorf("run %s is %s; nothing to resume", r.ID, r.Status)
	}
	r.Status = zzz.StatusRunning
	return r, nil
}

func zzzList() error {
	runs, err := zzz.ListRuns()
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Println("no zzz runs.")
		return nil
	}
	for _, r := range runs {
		fmt.Println(r.Summary())
	}
	return nil
}

func zzzCleanup(id string) error {
	r, err := zzz.LoadMeta(id)
	if err != nil {
		return err
	}
	if r.Mode != zzz.ModeWorktree || r.Worktree == "" {
		return fmt.Errorf("run %s is not a worktree run", id)
	}
	cwd, _ := os.Getwd()
	root, err := zzz.RepoRoot(cwd)
	if err != nil {
		return err
	}
	if err := zzz.WorktreeRemove(root, r.Worktree, true); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "removed worktree %s\n", r.Worktree)
	return nil
}

// installSigintHandler turns first SIGINT into a graceful "finish current
// iter and exit" signal; second SIGINT lets the default handler kill us.
func installSigintHandler(stopCh chan struct{}) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt)
	<-c
	close(stopCh)
	fmt.Fprintln(os.Stderr, "\n[zzz] SIGINT — will exit after current iteration. Ctrl+C again to force.")
	signal.Reset(os.Interrupt)
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

// buildZzzDeps mirrors the dependency wiring in runHeadlessReal up to the
// point of building the engine. Returns the bits zzz needs so each new
// engine can be constructed at run time (since Cwd differs in worktree mode).
func buildZzzDeps(model, provider, sandboxScope, thinking, effort, cavemanLvl string, yes bool) (
	config.Config, llm.Provider, approval.Approver, *skills.Registry, error,
) {
	cfg, err := config.Load()
	if err != nil {
		if os.Getenv("BEE_TEST_PROVIDER") != "" {
			cfg = config.Defaults()
		} else {
			return cfg, nil, nil, nil, fmt.Errorf("config: %w", err)
		}
	}
	applyOverrides(&cfg, model, provider, sandboxScope)
	effortVal := effort
	if effortVal == "" {
		effortVal = os.Getenv("BEE_EFFORT")
	}
	if thinking == "" && effortVal != "" {
		thinking = effortVal
	}
	if thinking != "" {
		cfg.Thinking = string(llm.ParseThinking(thinking))
	}
	cArg := cavemanLvl
	if cArg == "" {
		cArg = os.Getenv("BEE_CAVEMAN")
	}
	if cArg != "" {
		lvl, err := caveman.ParseLevel(cArg)
		if err != nil {
			return cfg, nil, nil, nil, err
		}
		cfg.Caveman = string(lvl)
	}
	prov, err := buildProvider(cfg)
	if err != nil {
		return cfg, nil, nil, nil, fmt.Errorf("provider: %w", err)
	}
	app := buildHeadlessApprover(cfg, yes)
	ensureFirstRun()
	skillReg := skills.NewRegistry()
	_ = skillReg.Load(skills.BaseDir())
	return cfg, prov, app, skillReg, nil
}

// buildZzzEngine assembles a *loop.Engine rooted at cwd (which may be a
// worktree). out routes engine deltas — pass os.Stdout for plain runs or
// the TUI's sink for live TUI mode. Returns a cleanup fn that closes the
// session rollout.
func buildZzzEngine(cfg config.Config, prov llm.Provider, app approval.Approver, skillReg *skills.Registry, cwd string, out io.Writer) (*loop.Engine, func(), error) {
	storeDir, _ := knowledge.StoreDir()
	reg, err := buildToolsWithApprover(cwd, cfg, prov, storeDir, app)
	if err != nil {
		return nil, func() {}, fmt.Errorf("tools: %w", err)
	}
	sessID := uuid.NewString()
	roll, err := session.Open(sessID)
	if err != nil {
		return nil, func() {}, fmt.Errorf("session: %w", err)
	}
	memStore := newKnowledgeAdapter(prov, cfg)
	if out == nil {
		out = os.Stdout
	}
	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Memory:   memStore,
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   out,
		Costs:    cost.New(),
	}
	return eng, func() { roll.Close() }, nil
}

// runGC prunes terminal zzz runs + done bg sessions per opts, prints what
// was removed. Worktrees are intentionally NOT touched here — those have
// their own lifecycle via `bee zzz --cleanup <id>` or `git worktree prune`.
func runGC(maxAge time.Duration, keep int) {
	zRes := zzz.Prune(zzz.PruneOpts{MaxAge: maxAge, KeepNewest: keep})
	bRes := bgreg.Prune(bgreg.PruneOpts{MaxAge: maxAge, KeepNewest: keep})
	fmt.Printf("zzz runs pruned: %d\n", len(zRes.RemovedRunIDs))
	for _, id := range zRes.RemovedRunIDs {
		fmt.Printf("  - %s\n", id)
	}
	fmt.Printf("bg sessions pruned: %d\n", len(bRes.RemovedIDs))
	for _, id := range bRes.RemovedIDs {
		fmt.Printf("  - %s\n", id)
	}
	for _, e := range append(zRes.Errors, bRes.Errors...) {
		fmt.Fprintf(os.Stderr, "gc warn: %v\n", e)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
