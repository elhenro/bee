package loop

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/beehome"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// runPlan is what a Run fixes before its first request: role and posture, the
// advertised tool surface, the system prompt and the user's text.
type runPlan struct {
	role       Role
	readOnly   bool
	noTools    bool
	specs      []llm.ToolSpec
	sys        string
	sysDynamic string
	userText   string
}

// planRun resolves role, tool surface and system prompt for a Run.
func (e *Engine) planRun(ctx context.Context, userText string) runPlan {
	// knowledge lookup: best-effort, never fatal.
	var recs []knowledge.Record
	if e.Memory != nil {
		r, err := e.Memory.Query(ctx, userText, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "loop: knowledge query failed: %v\n", err)
		}
		recs = r
	}

	// resolve role + per-turn posture. worker mode acts unless the user
	// explicitly requests a no-tool answer — no side-LLM classifier call.
	// The classifier existed to stop small local models from
	// reflex-shelling on a greeting, but on hosted providers it produced false
	// "plan" verdicts for clear edit requests (worker diagnosed, then stopped
	// without acting, forcing the user to send a follow-up). Local models still
	// get the full surface; a greeting produces a text-only turn that exits
	// cleanly. scout is always read-only; queen never reaches here (the TUI
	// routes queen turns through the hive).
	role := ParseRole(e.Cfg.Role)
	pl := runPlan{role: role, readOnly: role == RoleScout, noTools: noToolsRequested(userText), userText: userText}
	pl.specs = e.advertisedSpecs(pl)

	// walk-up AGENTS.md/CLAUDE.md plus ~/.bee global; best-effort.
	beeHome, _ := beehome.Dir()
	ctxFiles := prompt.LoadContextFiles(e.Cwd, beeHome)
	skillManifest := ""
	if e.Skills != nil {
		skillManifest = e.Skills.Manifest()
	}

	// reuse cached system prompt when the inputs fingerprint matches. saves
	// the Assemble + budget-trim work on every Run when nothing changed.
	cacheKey := sysPromptCacheKey(e.Cfg, e.Cwd, role, pl.readOnly, pl.specs, skillManifest, recs, ctxFiles)
	if pl.noTools {
		cacheKey = "" // rare explicit-stop turns must never reuse an acting prompt
	}
	if e.sysPromptCache.key == cacheKey && cacheKey != "" {
		pl.sys, pl.sysDynamic = e.sysPromptCache.value, e.sysPromptCache.dynamic
		return pl
	}
	pl.sys, pl.sysDynamic = prompt.AssembleWithDynamic(e.Cfg, e.Cwd, pl.specs, skillManifest, recs, ctxFiles)
	// rolePromptPrefix prepends, so the dynamic memory tail stays the suffix.
	if prefix := rolePromptPrefix(role, pl.readOnly); prefix != "" && !pl.noTools {
		pl.sys = prefix + "\n" + pl.sys
	}
	if pl.noTools {
		pl.sys = "## LATEST USER INSTRUCTION: NO TOOLS\n" +
			"Stop all tool use. Answer the latest user message directly from the existing conversation. " +
			"Do not continue the previous task, and do not emit tool calls even if earlier messages requested them.\n\n" + pl.sys
	}
	if cacheKey != "" {
		e.sysPromptCache.key, e.sysPromptCache.value, e.sysPromptCache.dynamic = cacheKey, pl.sys, pl.sysDynamic
	}
	return pl
}

// noToolsRequested recognizes an explicit request to stop working and answer.
// Keep this narrow: ordinary questions often need tools to answer correctly.
func noToolsRequested(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "stop" || strings.HasPrefix(t, "stop ") || strings.HasPrefix(t, "stop.") ||
		strings.HasPrefix(t, "stop,") || strings.HasPrefix(t, "stop!") || strings.HasPrefix(t, "please stop") {
		return true
	}
	return containsAny(t, "stop making tool calls", "stop using tools", "do not use tools", "don't use tools", "just answer")
}

// advertisedSpecs is the tool surface offered this Run, and the only one the
// executor accepts.
func (e *Engine) advertisedSpecs(pl runPlan) []llm.ToolSpec {
	specs := []llm.ToolSpec{}
	if e.Tools != nil && !pl.noTools {
		specs = e.Tools.Specs()
	}
	// drop user-disabled tools before any other filter
	specs = filterToolSpecsDisabled(specs, e.Cfg.DisabledTools)
	specs = addDisplayArgs(specs)
	// then narrow by role/posture: a read-only turn drops mutators entirely.
	specs = filterToolSpecsForRole(specs, pl.role, pl.readOnly)
	// re-add any plan-only tool a prompt skill granted for this turn (e.g.
	// /plan granting ask_user so it can prompt the user from edit/auto mode).
	if !pl.noTools {
		specs = applySkillToolGrants(specs, e.Tools, e.OnceAllowTools)
	}
	e.OnceAllowTools = nil
	// record the advertised surface so the executor can reject any tool the
	// model calls that wasn't offered this turn — read-only enforcement must
	// hold at execution time, not just at advertise time (a local model can
	// call write even when it's stripped from the wire).
	e.run.allowedTools = make(map[string]bool, len(specs))
	for _, s := range specs {
		e.run.allowedTools[s.Name] = true
	}
	return specs
}

// buildRequest assembles the provider request for one iteration of a Run.
func (e *Engine) buildRequest(ctx context.Context, pl runPlan, msgs []types.Message) llm.Request {
	// thinking is baked per-role when the user hasn't pinned an explicit
	// override (Cfg.Thinking empty). a CLI/env override sets a concrete
	// value that wins here.
	think := llm.ParseThinking(e.Cfg.Thinking)
	if strings.TrimSpace(e.Cfg.Thinking) == "" {
		think = RoleThinking(pl.role)
	}
	resolvedThinking := llm.ResolveThinking(think, e.Cfg.DefaultModel)
	sys := pl.sys
	// Qwen3 hybrid family (a3b, 235b) consumes `/think` / `/no_think`
	// via a literal system-prompt token instead of a reasoning_effort wire
	// field. read-only turn → /think (explicit reasoning); everything else →
	// /no_think (skip the reasoning trace — saves 200-2000 tokens per turn
	// on a sparse MoE). User-explicit Thinking=medium+ overrides.
	var templateKwargs map[string]any
	if llm.IsQwen3HybridThinking(e.Cfg.DefaultModel) {
		eff := resolvedThinking
		if pl.readOnly && eff == llm.ThinkingOff {
			eff = llm.ThinkingMedium
		}
		if hint := llm.Qwen3ThinkingHint(eff); hint != "" {
			sys = strings.TrimRight(sys, "\n") + "\n\n" + hint
		}
		// omlx/vllm honor the enable_thinking template switch; the
		// /no_think token above is ignored by some Qwen3 MLX templates.
		// local-only: hosted providers may reject chat_template_kwargs.
		if config.IsLocalProvider(e.Cfg.DefaultProvider) {
			templateKwargs = map[string]any{"enable_thinking": eff != llm.ThinkingOff}
		}
	}
	// suppression requested when effort resolved to off on a model that
	// nominally reasons. drives the post-turn compliance check.
	e.run.thinkingSuppressRequested = resolvedThinking == llm.ThinkingOff && llm.ThinkingApplies(e.Cfg.DefaultModel)
	// per-turn tail (hive pheromone card): re-evaluated every iteration so
	// state that moved since the last round is visible this round.
	sys, dynamic := appendPromptTail(sys, pl.sysDynamic, e.promptTail())
	wireMsgs := dropEphemeral(msgs)
	if e.run.card != nil {
		wireMsgs = e.run.card.view(wireMsgs)
	}
	return llm.Request{
		Model:              e.Cfg.DefaultModel,
		System:             sys,
		SystemDynamic:      dynamic,
		Messages:           capRecentImages(e.applyVisionFallback(ctx, wireMsgs)),
		Tools:              pl.specs,
		Stream:             true,
		MaxTokens:          config.OutputCap(e.Cfg),
		Thinking:           resolvedThinking,
		ChatTemplateKwargs: templateKwargs,
	}
}
