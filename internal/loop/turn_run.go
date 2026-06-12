package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	return e.RunWithContentDisplay(ctx, content, "")
}

// RunWithContentDisplay is RunWithContent with a render-only display label for
// the user turn. When display is non-empty the stored user message shows it in
// the TUI instead of Content (slash skills: typed command shown, expanded body
// sent). Empty display behaves exactly like RunWithContent.
func (e *Engine) RunWithContentDisplay(ctx context.Context, content []types.ContentBlock, display string) (RunResult, error) {
	if e.Stdout == nil {
		e.Stdout = os.Stdout
	}
	// flush the batched rollout fsync at the turn boundary so a completed Run is
	// durable even though per-message Append no longer fsyncs (see Rollout.Flush).
	if e.Sessions != nil {
		defer func() { _ = e.Sessions.Flush() }()
	}
	// reset per-Run state. freshRunState allocates the dedupe/streak maps;
	// sticky fields (sysPromptCache, profileScaledFor, warnedThinkingIgnored)
	// live on Engine and survive this assignment. The previous runState is
	// dropped — Run identity is the dedupe scope.
	e.run = e.freshRunState()
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

	// resolve role + per-turn posture. worker fires a side classifier off
	// userText to pick read-only vs act so small models don't reflex into shell
	// on a greeting. Local providers skip the classifier — the round-trip is
	// expensive on slow local models; default to act so tools stay available.
	// scout is always read-only; queen never reaches here (the TUI routes queen
	// turns through the hive).
	role := ParseRole(e.Cfg.Role)
	readOnly := role == RoleScout
	if role == RoleWorker && !e.SkipPostureClassifier {
		switch {
		case config.IsLocalProvider(e.Cfg.DefaultProvider):
			readOnly = false
		default:
			readOnly = classifyPosture(ctx, e.Provider, e.Cfg.DefaultModel, userText)
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
	// then narrow by role/posture: a read-only turn drops mutators entirely.
	specs = filterToolSpecsForRole(specs, role, readOnly)
	// re-add any plan-only tool a prompt skill granted for this turn (e.g.
	// /plan granting ask_user so it can prompt the user from edit/auto mode).
	specs = applySkillToolGrants(specs, e.Tools, e.OnceAllowTools)
	e.OnceAllowTools = nil
	// record the advertised surface so the executor can reject any tool the
	// model calls that wasn't offered this turn — read-only enforcement must
	// hold at execution time, not just at advertise time (a local model can
	// call write even when it's stripped from the wire).
	e.run.allowedTools = make(map[string]bool, len(specs))
	for _, s := range specs {
		e.run.allowedTools[s.Name] = true
	}
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
	cacheKey := sysPromptCacheKey(e.Cfg, role, readOnly, specs, skillManifest, recs, ctxFiles)
	var sys, sysDynamic string
	if e.sysPromptCache.key == cacheKey && cacheKey != "" {
		sys = e.sysPromptCache.value
		sysDynamic = e.sysPromptCache.dynamic
	} else {
		sys, sysDynamic = prompt.AssembleWithDynamic(e.Cfg, specs, skillManifest, recs, ctxFiles)
		// rolePromptPrefix prepends, so the dynamic memory tail stays the suffix.
		if prefix := rolePromptPrefix(role, readOnly); prefix != "" {
			sys = prefix + "\n" + sys
		}
		if cacheKey != "" {
			e.sysPromptCache.key = cacheKey
			e.sysPromptCache.value = sys
			e.sysPromptCache.dynamic = sysDynamic
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
	if e.compactionEnabled() {
		budget := contextBudget(e.Cfg)
		if ShouldAutoCompactWithUsage(sys, res.Messages, e.run.lastInputTokens, budget, scaledCompactThreshold(e.Cfg.Compaction.Threshold, budget)) && compactWorthwhile(res.Messages, budget) {
			if compacted, stats, cerr := e.compact(ctx, res.Messages); cerr == nil {
				res.Messages = e.emitCompactNotice(compacted, stats)
				e.run.lastInputTokens = 0
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
		Display:  display,
	}
	if err := e.appendMessage(ctx, userMessage); err != nil {
		return res, err
	}
	res.Messages = append(res.Messages, userMessage)

	// state card mode: per-Run card replaces the rolling transcript on the
	// wire (see statecard.go). reset every Run so the goal tracks the latest
	// user ask; the session transcript itself is untouched.
	if config.ActiveProfile(e.Cfg).StateCard {
		e.run.card = newStateCard(userText)
	} else {
		e.run.card = nil
	}

	// resolve the per-Run iteration ceiling. 0 (or negative) = unlimited: the
	// loop runs until a real guard fires (token budget or read-only stall).
	// config.Defaults() seeds config.DefaultMaxIterations, so 0 only appears when the user explicitly
	// lifted the cap (/iterations 0 or max_iterations = 0). A profile's
	// MaxIterations overrides the config value when set (0 = inherit).
	maxIter := e.Cfg.MaxIterations
	if p := config.ActiveProfile(e.Cfg); p.MaxIterations != 0 {
		maxIter = p.MaxIterations
	}
	unlimited := maxIter <= 0
	tokenBudget, stallCap := computeBudgetCaps(e.Cfg)
	for i := 0; unlimited || i < maxIter; i++ {
		if err := e.handleBudgetCaps(ctx, &res.Messages, i, tokenBudget, stallCap, readOnly); err != nil {
			return res, err
		}
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
		if e.compactionEnabled() {
			budget := contextBudget(e.Cfg)
			if ShouldAutoCompactWithUsage(sys, res.Messages, e.run.lastInputTokens, budget, scaledCompactThreshold(e.Cfg.Compaction.Threshold, budget)) && compactWorthwhile(res.Messages, budget) {
				if compacted, stats, cerr := e.compact(ctx, res.Messages); cerr == nil {
					res.Messages = e.emitCompactNotice(compacted, stats)
					// post-compact: reset lastInputTokens so the next
					// iteration re-evaluates against the smaller history.
					e.run.lastInputTokens = 0
				} else {
					fmt.Fprintf(os.Stderr, "loop: auto-compact failed: %v\n", cerr)
				}
			}
		}

		prof := config.ActiveProfile(e.Cfg)
		// thinking is baked per-role when the user hasn't pinned an explicit
		// override (Cfg.Thinking empty). a CLI/env override sets a concrete
		// value that wins here.
		think := llm.ParseThinking(e.Cfg.Thinking)
		if strings.TrimSpace(e.Cfg.Thinking) == "" {
			think = RoleThinking(role)
		}
		resolvedThinking := llm.ResolveThinking(think, e.Cfg.DefaultModel)
		reqSys := sys
		// Qwen3 hybrid family (a3b, 235b) consumes `/think` / `/no_think`
		// via a literal system-prompt token instead of a reasoning_effort wire
		// field. read-only turn → /think (explicit reasoning); everything else →
		// /no_think (skip the reasoning trace — saves 200-2000 tokens per turn
		// on a sparse MoE). User-explicit Thinking=medium+ overrides.
		var templateKwargs map[string]any
		if llm.IsQwen3HybridThinking(e.Cfg.DefaultModel) {
			eff := resolvedThinking
			if readOnly && eff == llm.ThinkingOff {
				eff = llm.ThinkingMedium
			}
			if hint := llm.Qwen3ThinkingHint(eff); hint != "" {
				reqSys = strings.TrimRight(reqSys, "\n") + "\n\n" + hint
			}
			// omlx/vllm honor the enable_thinking template switch; the
			// /no_think token above is ignored by some Qwen3 MLX templates.
			// local-only: hosted providers may reject chat_template_kwargs.
			if config.IsLocalProvider(e.Cfg.DefaultProvider) {
				templateKwargs = map[string]any{"enable_thinking": eff != llm.ThinkingOff}
			}
		}
		// suppression requested when effort resolved to off on a model that
		// nominally reasons. drives the post-turn compliance check below.
		e.run.thinkingSuppressRequested = resolvedThinking == llm.ThinkingOff && llm.ThinkingApplies(e.Cfg.DefaultModel)
		wireMsgs := dropEphemeral(res.Messages)
		if e.run.card != nil {
			wireMsgs = e.run.card.view(wireMsgs)
		}
		req := llm.Request{
			Model:              e.Cfg.DefaultModel,
			System:             reqSys,
			SystemDynamic:      sysDynamic,
			Messages:           e.applyVisionFallback(ctx, wireMsgs),
			Tools:              specs,
			Stream:             true,
			Temperature:        prof.Temperature,
			TopP:               prof.TopP,
			Thinking:           resolvedThinking,
			ChatTemplateKwargs: templateKwargs,
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

		// cross-turn reasoning loop: fold this turn's reasoning into the
		// duplicate streak so injectIterAndTokenWarnings can fire a hard nudge
		// when the model rehashes the same thinking turn after turn.
		observeReasoningDup(e, assistantMsg.Content)

		// effort-off compliance: warn once if the model reasoned despite the
		// suppression signal (some templates ignore /no_think + enable_thinking).
		e.verifyThinkingSuppression(finalText, assistantMsg)

		// stream was cut mid-repetition: nudge the model back on track, or bail
		// after loopCutBailAt consecutive cuts (wedged in a token loop).
		// when a tool call survived the cut, fall through: skipping dispatch
		// would leave an orphaned tool_use in the transcript (assistant msg is
		// already appended) and the wire rejects tool_use without tool_result.
		if e.run.lastTurnLooped {
			e.run.lastTurnLooped = false
			if len(toolUses) == 0 && !detectDoneSignal(finalText) {
				e.run.loopCutStreak++
				if e.run.loopCutStreak >= loopCutBailAt {
					return res, &RepeatStreamError{Streak: e.run.loopCutStreak}
				}
				nudge := types.Message{
					ID:       newID(),
					ParentID: assistantMsg.ID,
					Role:     types.RoleUser,
					Content: []types.ContentBlock{{Type: types.BlockText, Text: "[nudge] your output got stuck repeating the same text and was cut off. " +
						"stop repeating — call a tool to make progress or give a short final answer."}},
					Time: time.Now().UTC(),
				}
				if err := e.appendMessage(ctx, nudge); err != nil {
					return res, err
				}
				res.Messages = append(res.Messages, nudge)
				continue
			}
		}
		e.run.loopCutStreak = 0

		// stream dropped mid-output on a transient error. when no tool call
		// survived the drop, the turn made no progress — nudge the model to
		// continue (it sees its own partial output) and re-stream, bailing only
		// after truncCutBailAt consecutive no-progress drops (dead connection).
		// when a tool call DID survive, fall through: dispatching it is progress
		// and its result carries the loop forward on its own.
		if e.run.lastTurnTruncated {
			e.run.lastTurnTruncated = false
			if len(toolUses) == 0 && !detectDoneSignal(finalText) {
				e.run.truncCutStreak++
				if e.run.truncCutStreak >= truncCutBailAt {
					return res, &TruncatedStreamError{Streak: e.run.truncCutStreak}
				}
				nudge := types.Message{
					ID:       newID(),
					ParentID: assistantMsg.ID,
					Role:     types.RoleUser,
					Content: []types.ContentBlock{{Type: types.BlockText, Text: "[nudge] the connection dropped before you finished your last turn. " +
						"continue from where you left off — call a tool to make progress or give a short final answer."}},
					Time: time.Now().UTC(),
				}
				if err := e.appendMessage(ctx, nudge); err != nil {
					return res, err
				}
				res.Messages = append(res.Messages, nudge)
				continue
			}
		}
		e.run.truncCutStreak = 0

		// a turn with any text or tool call clears the empty-completion streak.
		if strings.TrimSpace(finalText) != "" || len(toolUses) > 0 {
			e.run.emptyCompletionStreak = 0
		}

		if len(toolUses) == 0 || detectDoneSignal(finalText) {
			// whitespace-only turn: no text, no reasoning, no tool. nudge once,
			// then bail with a typed error so the wrapper can surface "switch
			// model" instead of ending on a blank answer or spinning the budget.
			if strings.TrimSpace(finalText) == "" && len(toolUses) == 0 && !hasThinkingOnly(assistantMsg) {
				e.run.emptyCompletionStreak++
				if e.run.emptyCompletionStreak >= emptyCompletionBailAt {
					return res, &EmptyCompletionError{Streak: e.run.emptyCompletionStreak}
				}
				nudge := types.Message{
					ID:       newID(),
					ParentID: assistantMsg.ID,
					Role:     types.RoleUser,
					Content: []types.ContentBlock{{Type: types.BlockText, Text: "[nudge] your last turn was empty — no answer and no tool call. " +
						"respond now: call a tool to make progress or give a short final answer."}},
					Time: time.Now().UTC(),
				}
				if err := e.appendMessage(ctx, nudge); err != nil {
					return res, err
				}
				res.Messages = append(res.Messages, nudge)
				continue
			}
			// format-slip streak: a turn with zero tool_uses but text that
			// looks like an attempted call counts toward the strike budget.
			// done-signal turns are intentional exits, not slips. real done
			// resets the streak so the FormatStrikeError reads as consecutive.
			if len(toolUses) == 0 && !detectDoneSignal(finalText) && looksLikeAttemptedToolCall(finalText, specs) {
				e.run.formatSlipStreak++
				if e.run.formatSlipStreak >= formatStrikeAt {
					return res, &FormatStrikeError{Streak: e.run.formatSlipStreak}
				}
			} else {
				e.run.formatSlipStreak = 0
			}
			if nudge := attemptRecoveryNudge(e, assistantMsg, finalText, toolUses, specs); nudge != nil {
				if err := e.appendMessage(ctx, *nudge); err != nil {
					return res, err
				}
				res.Messages = append(res.Messages, *nudge)
				continue
			}
			// say-only stop guard: first turn, no tool call, task-looking
			// request — push once instead of ending the Run with nothing done.
			// i == 0 bounds it to one nudge per Run by construction.
			if i == 0 && !readOnly && !detectDoneSignal(finalText) {
				if nudge := sayOnlyStopNudge(e, assistantMsg, userText); nudge != nil {
					if err := e.appendMessage(ctx, *nudge); err != nil {
						return res, err
					}
					res.Messages = append(res.Messages, *nudge)
					continue
				}
			}
			return res, nil
		}
		// any successful tool dispatch resets the format-slip streak; the model
		// proved it can produce a parseable envelope this turn.
		e.run.formatSlipStreak = 0

		// one-time confirmation that constrained json tool mode is live: the
		// first parsed call proves the grammar pipeline works end to end.
		if !e.jsonModeNoticeShown && strings.HasSuffix(e.Provider.Name(), "+jsonmode") {
			e.jsonModeNoticeShown = true
			e.noticef("json tool mode active · first tool call parsed clean")
		}

		// dispatch tools (read-only ones run in parallel, mutators serial)
		toolResults, err := e.dispatchTools(ctx, toolUses)
		if err != nil {
			return res, err
		}
		if e.run.card != nil {
			e.run.card.observe(toolUses, toolResults)
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
			e.run.noMutationStreak = 0
		} else {
			e.run.noMutationStreak++
		}

		blocks := toolResultBlocks(toolResults)
		blocks, repeatErr := observeRepeats(e, toolUses, toolResults, blocks)
		blocks = observeDuplicateWrites(e, toolUses, toolResults, blocks)
		blocks = observeEditNoVerify(e, toolUses, toolResults, blocks)
		blocks = injectIterAndTokenWarnings(e, blocks, i+1, maxIter, tokenBudget, readOnly)
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
		// two-strike bail: append the result first (so transcript shows what
		// failed) then surface the typed error to the caller.
		if repeatErr != nil {
			return res, repeatErr
		}
		// escalate bail: same shape — append the synthetic tool_result first
		// (transcript shows the model's reason) then surface ErrEscalate.
		if e.run.escalateErr != nil {
			esc := e.run.escalateErr
			e.run.escalateErr = nil
			return res, &EscalateError{Reason: esc.Reason, NextAction: esc.NextAction, Options: esc.Options}
		}
	}
	// graceful wind-down: one tool-less turn so the user gets a close-out
	// summary rather than a bare cap error. best-effort, never blocks the exit.
	e.finalSummaryOnCap(ctx, &res, sys, sysDynamic)
	return res, &MaxIterationsError{Limit: maxIter}
}
