package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// RunWithContent is Run with a pre-built content slice. Used by the TUI when
// staging multimodal input (e.g. images via Ctrl+I) so the user message can
// carry text + image blocks together.
func (e *Engine) RunWithContent(ctx context.Context, content []types.ContentBlock) (RunResult, error) {
	if e.Stdout == nil {
		e.Stdout = os.Stdout
	}
	// reset per-Run state so context warnings dedupe inside one Run only.
	e.lastInputTokens = 0
	e.warnedContext = false
	e.warnedIterHalf = false
	e.warnedIterEighty = false
	e.warnedStall = false
	e.noMutationStreak = 0
	e.nudgedReasoningOnly = false
	res := RunResult{}

	// probe the active model's context window before the first iteration so
	// auto-compact knows the real budget for novel models the hardcoded table
	// doesn't carry (e.g. fresh ollama pulls, deepseek-v4-pro, lm-studio
	// custom configs). Best-effort; dedupes per (provider,model) via probe.go.
	if pc, ok := e.Cfg.Providers[e.Cfg.DefaultProvider]; ok {
		_ = llm.ProbeContextLength(ctx, e.Cfg.DefaultProvider, pc, e.Cfg.DefaultModel)
	}
	// scale tiny-profile budgets up when the active model has much more
	// context than the 4k default tiny assumes (sparse MoE: Qwen3-A3B-128k,
	// etc.). Re-runs on model switch via profileScaledFor sentinel.
	if e.profileScaledFor != e.Cfg.DefaultModel {
		if name := config.ResolveAutoProfileForProvider(e.Cfg.DefaultProvider, e.Cfg.DefaultModel); name == "tiny" {
			if ctxWindow := llm.ContextWindow(e.Cfg.DefaultModel); ctxWindow > 16000 {
				e.Cfg.Profiles = cloneProfiles(e.Cfg.Profiles)
				resolved := e.Cfg.Profile
				if resolved == "auto" {
					resolved = name
				}
				if p, ok := e.Cfg.Profiles[resolved]; ok {
					e.Cfg.Profiles[resolved] = config.ScaleProfileForContext(p, resolved, ctxWindow)
				}
			}
		}
		e.profileScaledFor = e.Cfg.DefaultModel
	}

	// `@path` expansion: inline file contents for any `@<rel>` token the
	// user typed. Applied to text blocks only; image blocks pass through.
	content = expandAtPathsInContent(content, e.Cwd)

	// flatten text blocks for knowledge-store query; non-text content is
	// ignored (the store works on plain strings).
	userText := collectUserText(content)

	// knowledge lookup: best-effort, never fatal.
	var recs []knowledge.Record
	if e.Memory != nil {
		r, err := e.Memory.Query(ctx, userText, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "loop: knowledge query failed: %v\n", err)
		}
		recs = r
	}

	// resolve effective mode: auto fires a side classifier off userText.
	// Tiny profile uses plan-first instead — small non-reasoning models can't
	// be coaxed into hidden CoT via reasoning_effort, so we force an explicit
	// plan turn before any edits run (see planFirst flow below). Local
	// non-tiny providers skip the classifier — round-trip is expensive on
	// slow local models; default to edit so tools stay available.
	mode := ParseMode(e.Cfg.Mode)
	planFirst := false
	if mode == ModeAuto {
		switch {
		case e.Cfg.Profile == "tiny":
			mode = ModePlan
			planFirst = true
		case config.IsLocalProvider(e.Cfg.DefaultProvider):
			mode = ModeEdit
		default:
			mode = ClassifyMode(ctx, e.Provider, e.Cfg.DefaultModel, userText)
		}
	}

	specs := []llm.ToolSpec{}
	if e.Tools != nil {
		specs = e.Tools.Specs()
	}
	// drop user-disabled tools before any other filter
	specs = filterToolSpecsDisabled(specs, e.Cfg.DisabledTools)
	// trim tool surface for tiny-profile models — see filterToolSpecsForProfile.
	// user_tools force-pass the profile gate so they're always advertised when not disabled.
	extras := append([]string(nil), e.Cfg.ExtraTools...)
	for _, u := range e.Cfg.UserTools {
		extras = append(extras, u.Name)
	}
	specs = filterToolSpecsForProfile(specs, e.Cfg.Profile, extras...)
	// strip per-parameter descriptions on tiny: saves ~600 toks for 4k models.
	// no-op when the profile uses tool_format=xml (schema is nilled by the
	// textmode wrapper before it reaches the wire).
	specs = stripToolSpecDescriptionsForProfile(specs, e.Cfg)
	// then narrow by mode: plan mode drops mutators entirely. Keep the
	// pre-mode spec list so the plan-first flow can restore mutators when
	// it transitions to edit mid-Run without re-running the upstream filters.
	specsPreMode := specs
	specs = filterToolSpecsForMode(specs, mode)
	skillManifest := ""
	if e.Skills != nil {
		skillManifest = e.Skills.Manifest()
	}

	// walk-up AGENTS.md/CLAUDE.md plus ~/.bee global; best-effort.
	beeHome := ""
	if home, err := os.UserHomeDir(); err == nil {
		beeHome = filepath.Join(home, ".bee")
	}
	ctxFiles := prompt.LoadContextFiles(e.Cwd, beeHome)

	// reuse cached system prompt when the inputs fingerprint matches. saves
	// the Assemble + budget-trim work on every Run when nothing changed.
	cacheKey := sysPromptCacheKey(e.Cfg, mode, specs, skillManifest, recs, ctxFiles)
	var sys string
	if e.sysPromptCache.key == cacheKey && cacheKey != "" {
		sys = e.sysPromptCache.value
	} else {
		sys = prompt.Assemble(e.Cfg, specs, skillManifest, recs, ctxFiles)
		if prefix := modePromptPrefix(mode); prefix != "" {
			sys = prefix + "\n" + sys
		}
		if cacheKey != "" {
			e.sysPromptCache.key = cacheKey
			e.sysPromptCache.value = sys
		}
	}

	// seed prior turns so multi-turn / resumed sessions have full context.
	// not re-persisted: caller owns disk state.
	if len(e.InitialMessages) > 0 {
		res.Messages = append(res.Messages, e.InitialMessages...)
	}

	// pre-compact: free budget BEFORE appending the new user turn so the
	// upcoming request has headroom. uses lastInputTokens from the prior
	// run when available; otherwise falls back to estimator over sys+history.
	if e.Cfg.Compaction.Enabled {
		budget := contextBudget(e.Cfg)
		if ShouldAutoCompactWithUsage(sys, res.Messages, e.lastInputTokens, budget, scaledCompactThreshold(e.Cfg.Compaction.Threshold, budget)) {
			if compacted, _, cerr := Compact(ctx, e.Provider, e.Cfg.DefaultModel, res.Messages); cerr == nil {
				res.Messages = compacted
				e.lastInputTokens = 0
			} else {
				fmt.Fprintf(os.Stderr, "loop: auto-compact failed: %v\n", cerr)
			}
		}
	}

	// append user message; chain ParentID to last seeded msg when resuming
	userMessage := types.Message{
		ID:       newID(),
		ParentID: lastID(res.Messages),
		Role:     types.RoleUser,
		Content:  content,
		Time:     time.Now().UTC(),
	}
	if err := e.appendMessage(ctx, userMessage); err != nil {
		return res, err
	}
	res.Messages = append(res.Messages, userMessage)

	maxIter := MaxIterations
	if e.Cfg.MaxIterations > 0 {
		maxIter = e.Cfg.MaxIterations
	}
	// profile override wins so tiny-profile small models fail-fast at 12
	// rather than chewing through the 50-default budget.
	if p := config.ActiveProfile(e.Cfg); p.MaxIterations > 0 {
		maxIter = p.MaxIterations
	}
	for i := 0; i < maxIter; i++ {
		// mid-turn steering: drain pending user input into a synthetic
		// user message before the next LLM round. Non-blocking so quiet
		// iterations don't stall.
		if e.SteerCh != nil {
			select {
			case steer := <-e.SteerCh:
				if strings.TrimSpace(steer) != "" {
					steerMsg := types.Message{
						ID:       newID(),
						ParentID: lastID(res.Messages),
						Role:     types.RoleUser,
						Content:  []types.ContentBlock{{Type: types.BlockText, Text: "[steer] " + steer}},
						Time:     time.Now().UTC(),
					}
					if err := e.appendMessage(ctx, steerMsg); err == nil {
						res.Messages = append(res.Messages, steerMsg)
					}
				}
			default:
			}
		}

		// auto-compact when prompt+history nears the budget. Check at loop
		// top so newly-appended tool/assistant messages from prior iteration
		// are also accounted for. Prefer the provider's last reported input
		// token count (most accurate, works for every wire format that
		// surfaces usage); fall back to the estimator on the first turn.
		if e.Cfg.Compaction.Enabled {
			budget := contextBudget(e.Cfg)
			if ShouldAutoCompactWithUsage(sys, res.Messages, e.lastInputTokens, budget, scaledCompactThreshold(e.Cfg.Compaction.Threshold, budget)) {
				if compacted, _, cerr := Compact(ctx, e.Provider, e.Cfg.DefaultModel, res.Messages); cerr == nil {
					res.Messages = compacted
					// post-compact: reset lastInputTokens so the next
					// iteration re-evaluates against the smaller history.
					e.lastInputTokens = 0
				} else {
					fmt.Fprintf(os.Stderr, "loop: auto-compact failed: %v\n", cerr)
				}
			}
		}

		prof := config.ActiveProfile(e.Cfg)
		resolvedThinking := llm.ResolveThinking(llm.ParseThinking(e.Cfg.Thinking), e.Cfg.DefaultModel)
		reqSys := sys
		// Qwen3 hybrid family (a3b, coder, 235b) consumes `/think` / `/no_think`
		// via a literal system-prompt token instead of a reasoning_effort wire
		// field. Plan mode → /think (explicit reasoning); everything else →
		// /no_think (skip the reasoning trace — saves 200-2000 tokens per turn
		// on a sparse MoE). User-explicit Thinking=medium+ overrides.
		if llm.IsQwen3HybridThinking(e.Cfg.DefaultModel) {
			eff := resolvedThinking
			if mode == ModePlan && eff == llm.ThinkingOff {
				eff = llm.ThinkingMedium
			}
			if hint := llm.Qwen3ThinkingHint(eff); hint != "" {
				reqSys = strings.TrimRight(reqSys, "\n") + "\n\n" + hint
			}
		}
		req := llm.Request{
			Model:       e.Cfg.DefaultModel,
			System:      reqSys,
			Messages:    res.Messages,
			Tools:       specs,
			Stream:      true,
			Temperature: prof.Temperature,
			TopP:        prof.TopP,
			Thinking:    resolvedThinking,
		}
		assistantMsg, finalText, toolUses, err := e.streamOnce(ctx, req)
		if err != nil {
			return res, err
		}
		assistantMsg.ParentID = lastID(res.Messages)
		if err := e.appendMessage(ctx, assistantMsg); err != nil {
			return res, err
		}
		res.Messages = append(res.Messages, assistantMsg)
		res.FinalText = finalText

		// plan-first transition: tiny+auto starts in ModePlan so the model
		// emits an explicit plan before any edits. Once plan turn lands a
		// text reply with no tool calls (and the user didn't ask to stop),
		// swap to ModeEdit, restore mutators, drop the plan prefix, and
		// inject a synthetic nudge telling the model to execute the plan.
		if planFirst && mode == ModePlan && len(toolUses) == 0 && !detectDoneSignal(finalText) {
			planFirst = false
			mode = ModeEdit
			specs = filterToolSpecsForMode(specsPreMode, ModeEdit)
			sys = prompt.Assemble(e.Cfg, specs, skillManifest, recs, ctxFiles)
			cont := types.Message{
				ID:       newID(),
				ParentID: assistantMsg.ID,
				Role:     types.RoleUser,
				Content: []types.ContentBlock{{Type: types.BlockText, Text: "[plan-first] plan above is approved. execute it now: call tools to make the changes step by step."}},
				Time: time.Now().UTC(),
			}
			if err := e.appendMessage(ctx, cont); err != nil {
				return res, err
			}
			res.Messages = append(res.Messages, cont)
			continue
		}

		if len(toolUses) == 0 || detectDoneSignal(finalText) {
			// stall recovery: turn ended with no tool calls and no done signal.
			// two failure modes, both nudged once with a synthetic user turn:
			//   1. thinking-only — provider streamed reasoning then stopped
			//      without text or tool_use (e.g. some hosted reasoners).
			//   2. format slip — model emitted a tool-call attempt as prose
			//      (XML-ish tag or JSON envelope) but the textmode parser
			//      didn't recognize it. silent termination here leaves the
			//      user staring at JSON in the transcript; nudge with an
			//      explicit format reminder instead.
			if !e.nudgedReasoningOnly && len(toolUses) == 0 && !detectDoneSignal(finalText) {
				nudgeText := ""
				switch {
				case strings.TrimSpace(finalText) == "" && hasThinkingOnly(assistantMsg):
					nudgeText = "[nudge] previous turn was reasoning-only. respond now: emit final answer or call a tool."
				case looksLikeAttemptedToolCall(finalText, specs):
					nudgeText = "[nudge] previous turn looked like a tool call but the envelope was wrong. tools must be invoked as `<tool_name>{\"arg\":\"value\"}</tool_name>` XML blocks, not bare JSON or function-call objects. retry with the XML envelope."
				}
				if nudgeText != "" {
					e.nudgedReasoningOnly = true
					nudge := types.Message{
						ID:       newID(),
						ParentID: assistantMsg.ID,
						Role:     types.RoleUser,
						Content:  []types.ContentBlock{{Type: types.BlockText, Text: nudgeText}},
						Time:     time.Now().UTC(),
					}
					if err := e.appendMessage(ctx, nudge); err != nil {
						return res, err
					}
					res.Messages = append(res.Messages, nudge)
					continue
				}
			}
			return res, nil
		}

		// dispatch tools (read-only ones run in parallel, mutators serial)
		toolResults, err := e.dispatchTools(ctx, toolUses)
		if err != nil {
			return res, err
		}
		// track mutation cadence for stall detection
		mutated := false
		for _, u := range toolUses {
			if mutatorTools[u.Name] {
				mutated = true
				break
			}
		}
		if mutated {
			e.noMutationStreak = 0
		} else {
			e.noMutationStreak++
		}

		blocks := toolResultBlocks(toolResults)
		// context-window warning: if usage crosses threshold, prepend a
		// one-shot notice to the next tool result message so the model
		// summarizes/drops noise on the following turn. dedupes per Run.
		if !e.warnedContext {
			limit := contextBudget(e.Cfg)
			if w := prompt.FormatContextWarning(e.lastInputTokens, limit); w != "" {
				blocks = prependWarningToToolResult(blocks, w)
				e.warnedContext = true
			}
		}
		// iteration / stall warnings; each fires at most once per Run.
		// iter is 0-based; current = i+1 (we've just finished one round).
		current := i + 1
		if !e.warnedIterHalf && current*2 >= maxIter {
			w := fmt.Sprintf("[iter %d/%d] half the budget spent. summarize progress; commit edits or stop if stuck.\n\n", current, maxIter)
			blocks = prependWarningToToolResult(blocks, w)
			e.warnedIterHalf = true
		}
		if !e.warnedIterEighty && current*5 >= maxIter*4 {
			w := fmt.Sprintf("[iter %d/%d] near iter cap. finish current edit or stop and ask user.\n\n", current, maxIter)
			blocks = prependWarningToToolResult(blocks, w)
			e.warnedIterEighty = true
		}
		// stall warning is opt-in: profile must set a positive threshold.
		if t := config.ActiveProfile(e.Cfg).NoMutationStallThreshold; t > 0 {
			if !e.warnedStall && e.noMutationStreak >= t {
				w := fmt.Sprintf("[stall] %d read-only iters; commit edits when ready.\n\n", e.noMutationStreak)
				blocks = prependWarningToToolResult(blocks, w)
				e.warnedStall = true
			}
		}
		toolMsg := types.Message{
			ID:       newID(),
			ParentID: assistantMsg.ID,
			Role:     types.RoleTool,
			Content:  blocks,
			Time:     time.Now().UTC(),
		}
		if err := e.appendMessage(ctx, toolMsg); err != nil {
			return res, err
		}
		res.Messages = append(res.Messages, toolMsg)
	}
	return res, fmt.Errorf("loop: hit max iterations (%d) — type 'continue' to resume", maxIter)
}

// sysPromptCacheKey builds a cheap fingerprint of the inputs to prompt.Assemble.
// inputs include cwd + mode + profile + caveman level + tool specs (name+desc
// lengths) + skill manifest length + record names/sizes + ctx-file paths/lengths.
// returns "" if any required input is unstable enough to skip caching.
func sysPromptCacheKey(cfg config.Config, mode Mode, specs []llm.ToolSpec, skillManifest string, recs []knowledge.Record, ctxFiles []prompt.ContextFile) string {
	var b strings.Builder
	b.WriteString(cfg.DefaultProvider)
	b.WriteByte('|')
	b.WriteString(cfg.DefaultModel)
	b.WriteByte('|')
	b.WriteString(cfg.Profile)
	b.WriteByte('|')
	b.WriteString(cfg.Caveman)
	b.WriteByte('|')
	b.WriteString(string(mode))
	b.WriteByte('|')
	for _, s := range specs {
		b.WriteString(s.Name)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(s.Description)))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(s.PromptSnippet)))
		b.WriteByte(';')
	}
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(len(skillManifest)))
	b.WriteByte('|')
	for _, r := range recs {
		b.WriteString(r.Name)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(r.Body)))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(r.Modified.UnixNano(), 10))
		b.WriteByte(';')
	}
	b.WriteByte('|')
	for _, f := range ctxFiles {
		b.WriteString(f.Path)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(f.Body)))
		b.WriteByte(';')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}
