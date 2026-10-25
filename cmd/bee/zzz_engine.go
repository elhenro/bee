package main

import (
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/zzz"
)

// buildZzzDeps mirrors the dependency wiring in runHeadless up to the point of
// building the engine. Returns the bits zzz needs so each new engine can be
// constructed at run time (since Cwd differs in worktree mode).
//
// The approval policy never reads stdin: an unattended overnight run with no
// one to answer would wedge the loop forever (and in TUI mode contends with
// bubbletea for the tty). With autoYes flagged-dangerous commands run;
// otherwise they are denied so the model picks another approach instead of
// blocking. Non-dangerous commands never reach the approver either way.
func buildZzzDeps(model, provider, sandboxScope, thinking, effort, cavemanLvl string, autoYes bool) (
	config.Config, llm.Provider, approval.Approver, *skills.Registry, error,
) {
	cfg, err := loadConfig(model, provider, sandboxScope)
	if err != nil {
		return cfg, nil, nil, nil, fmt.Errorf("config: %w", err)
	}
	if err := applyThinkingCaveman(&cfg, thinking, effort, cavemanLvl); err != nil {
		return cfg, nil, nil, nil, err
	}
	prov, err := buildProvider(cfg)
	if err != nil {
		return cfg, nil, nil, nil, fmt.Errorf("provider: %w", err)
	}
	verdict := approval.Deny
	if autoYes {
		verdict = approval.AllowOnce
	}
	ensureFirstRun()
	return cfg, prov, approval.Static{Verdict: verdict}, newSkillRegistry(), nil
}

// resolveZzzBudgets turns the auto sentinels into concrete caps. Small/local
// ("tiny") models get tighter defaults because they spin more; explicit flag
// values (iter>0, tok>=0) always win. Returns (maxIterations, maxTokens) where
// maxTokens 0 means unlimited.
func resolveZzzBudgets(cfg config.Config, maxIter, maxTok int) (int, int) {
	prof := cfg.Profile
	if prof == "" || prof == "auto" {
		prof = config.ResolveAutoProfileForProvider(cfg.DefaultProvider, cfg.DefaultModel)
	}
	tiny := prof == "tiny"
	if maxIter <= 0 {
		if tiny {
			maxIter = 40
		} else {
			maxIter = loop.MaxIterations
		}
	}
	if maxTok < 0 {
		if tiny {
			maxTok = 400000
		} else {
			maxTok = 0
		}
	}
	return maxIter, maxTok
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
	roll, err := session.Open(uuid.NewString())
	if err != nil {
		return nil, func() {}, fmt.Errorf("session: %w", err)
	}
	if out == nil {
		out = os.Stdout
	}
	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Waggle:   waggleManagerLearning(cfg, waggleProject(cwd)),
		Memory:   newKnowledgeAdapter(prov, cfg),
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   out,
		Costs:    cost.New(),
	}
	return eng, func() { roll.Close() }, nil
}

// waggleProject keys the waggle store by the repo, not by the run's worktree:
// a worktree path is unique per run, so routes mined there would be orphaned
// the moment the run ends.
func waggleProject(cwd string) string {
	if root, err := zzz.MainRepoRoot(cwd); err == nil {
		return root
	}
	return cwd
}
