// Wiring shared by the commands that launch an engine: the common flags, the
// config they resolve to, and the engine shape the headless paths share.
package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/skillexec"
)

// engineFlags are the flags every engine-launching command shares.
type engineFlags struct {
	model, provider, sandbox, thinking, effort, caveman string
	safe, yes, yolo                                     bool
}

// register declares the model, sandbox and prompt-shaping flags.
func (f *engineFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.model, "model", "", "override config default_model")
	fs.StringVar(&f.provider, "provider", "", "override config default_provider")
	fs.StringVar(&f.sandbox, "sandbox", "", "override sandbox scope (read-only|workspace-write|workspace-write-net|danger-full-access)")
	fs.BoolVar(&f.safe, "safe", false, "shorthand for --sandbox workspace-write: confine writes to cwd+tmp, block network")
	fs.StringVar(&f.thinking, "thinking", "", "thinking level: auto|off|low|medium|high|max (default: from config)")
	fs.StringVar(&f.effort, "effort", "", "alias for --thinking: auto|off|low|medium|high|max")
	fs.StringVar(&f.caveman, "caveman", "", "force caveman level: off|lite|full|ultra")
}

// registerAutoApprove declares --yes and --yolo for commands where either one
// means "run flagged shell commands without asking".
func (f *engineFlags) registerAutoApprove(fs *flag.FlagSet) {
	fs.BoolVar(&f.yes, "yes", false, "auto-approve any dangerous shell command without prompting (still blocks hardline-refused commands)")
	fs.BoolVar(&f.yolo, "yolo", false, "alias for --yes: auto-approve dangerous commands")
}

func (f *engineFlags) autoApprove() bool { return f.yes || f.yolo }

// load resolves config with the flags applied. defScope is the sandbox scope
// when neither --sandbox nor --safe is given; "" keeps the configured one.
func (f *engineFlags) load(defScope string) (config.Config, error) {
	cfg, err := loadConfig(f.model, f.provider, resolveSafe(f.sandbox, f.safe, defScope))
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	return cfg, applyThinkingCaveman(&cfg, f.thinking, f.effort, f.caveman)
}

// mustLoad is load for a CLI entry point: a config failure exits 1, a bad
// --thinking or --caveman value exits 2.
func (f *engineFlags) mustLoad(cmd, defScope string) config.Config {
	cfg := mustConfig(cmd, f.model, f.provider, resolveSafe(f.sandbox, f.safe, defScope))
	if err := applyThinkingCaveman(&cfg, f.thinking, f.effort, f.caveman); err != nil {
		exitf(2, "%s: %v", cmd, err)
	}
	return cfg
}

// registerSkillTools exposes exec-kind skills (including waggles) as
// model-callable tools, confined to the active sandbox scope. Call it before
// any --allowed-tools filter so the allowlist governs them too.
func registerSkillTools(reg *tools.Registry, skillReg *skills.Registry, cfg config.Config, cwd string) {
	skillexec.RegisterExecSkills(reg, skillReg.List(), execSandbox(cfg, cwd)...)
}

// newHeadlessEngine builds the engine the single-agent headless commands
// share: knowledge memory, cost tracking and a session rollout, streaming to
// out. The caller adds what is its own (waggle, jobs, host session).
func newHeadlessEngine(prov llm.Provider, reg *tools.Registry, skillReg *skills.Registry, cfg config.Config, cwd string, roll *session.Rollout, out io.Writer) *loop.Engine {
	return &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Memory:   newKnowledgeAdapter(prov, cfg),
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   out,
		Costs:    cost.New(),
	}
}
