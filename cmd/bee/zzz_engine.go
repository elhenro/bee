package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/gate"
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
// bubbletea for the tty). With --yes/--yolo flagged-dangerous commands run;
// otherwise they are denied so the model picks another approach instead of
// blocking. Non-dangerous commands never reach the approver either way.
func buildZzzDeps(ef engineFlags) (
	config.Config, llm.Provider, approval.Approver, *skills.Registry, error,
) {
	// unattended: confine to the workspace unless --sandbox says otherwise
	cfg, err := ef.load("workspace-write")
	if err != nil {
		return cfg, nil, nil, nil, err
	}
	prov, err := buildProvider(cfg)
	if err != nil {
		return cfg, nil, nil, nil, fmt.Errorf("provider: %w", err)
	}
	verdict := approval.Deny
	if ef.autoApprove() {
		verdict = approval.AllowOnce
	}
	ensureFirstRun()
	return cfg, prov, approval.Static{Verdict: verdict}, newSkillRegistry(), nil
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
	registerSkillTools(reg, skillReg, cfg, cwd)
	eng := newHeadlessEngine(prov, reg, skillReg, cfg, cwd, roll, out)
	eng.Waggle = waggleManagerLearning(cfg, waggleProject(cwd))
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

// zzzGate resolves the verify commands for the repo under cwd and says which
// gate the run will use, so an ungated overnight run is never a surprise.
func zzzGate(cfg config.Config) []string {
	cwd, _ := os.Getwd()
	root, err := zzz.RepoRoot(cwd)
	if err != nil {
		root = cwd
	}
	cmds := gate.Commands(root, cfg.Swarm.Verify)
	if len(cmds) == 0 {
		fmt.Fprintf(os.Stderr, "[zzz] no verify gate (%s or [swarm] verify): commits land untested\n", gate.File)
		return nil
	}
	fmt.Fprintf(os.Stderr, "[zzz] verify gate: %s\n", strings.Join(cmds, " && "))
	return cmds
}
