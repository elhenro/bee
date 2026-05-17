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
	// Tiny profile + local providers skip the classifier — round-trip is
	// expensive on slow local models and small 7B models hallucinate the
	// answer; default to edit so tools stay available.
	mode := ParseMode(e.Cfg.Mode)
	if mode == ModeAuto {
		if e.Cfg.Profile == "tiny" || config.IsLocalProvider(e.Cfg.DefaultProvider) {
			mode = ModeEdit
		} else {
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
	// strip per-parameter descriptions on tiny — saves ~600 toks for 4k models
	specs = stripToolSpecDescriptionsForProfile(specs, e.Cfg.Profile)
	// then narrow by mode: plan mode drops mutators entirely.
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

	sys := prompt.Assemble(e.Cfg, specs, skillManifest, recs, ctxFiles)
	if prefix := modePromptPrefix(mode); prefix != "" {
		sys = prefix + "\n" + sys
	}

	// seed prior turns so multi-turn / resumed sessions have full context.
	// not re-persisted: caller owns disk state.
	if len(e.InitialMessages) > 0 {
		res.Messages = append(res.Messages, e.InitialMessages...)
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
			if ShouldAutoCompactWithUsage(sys, res.Messages, e.lastInputTokens, budget, e.Cfg.Compaction.Threshold) {
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

		req := llm.Request{
			Model:    e.Cfg.DefaultModel,
			System:   sys,
			Messages: res.Messages,
			Tools:    specs,
			Stream:   true,
			Thinking: llm.ResolveThinking(llm.ParseThinking(e.Cfg.Thinking), e.Cfg.DefaultModel),
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

		if len(toolUses) == 0 || detectDoneSignal(finalText) {
			// reasoning-only stall: provider emitted a thinking block but no
			// text and no tool_use. some hosted reasoners (deepseek-v4-flash
			// via OpenRouter) hit this. nudge once with a synthetic user turn
			// so the loop can recover without silent termination.
			if !e.nudgedReasoningOnly && len(toolUses) == 0 &&
				strings.TrimSpace(finalText) == "" && hasThinkingOnly(assistantMsg) {
				e.nudgedReasoningOnly = true
				nudge := types.Message{
					ID:       newID(),
					ParentID: assistantMsg.ID,
					Role:     types.RoleUser,
					Content:  []types.ContentBlock{{Type: types.BlockText, Text: "[nudge] previous turn was reasoning-only. respond now: emit final answer or call a tool."}},
					Time:     time.Now().UTC(),
				}
				if err := e.appendMessage(ctx, nudge); err != nil {
					return res, err
				}
				res.Messages = append(res.Messages, nudge)
				continue
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
