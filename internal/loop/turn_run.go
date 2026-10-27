package loop

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/herdr"
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
	// tell the hosting herdr pane a turn is running (no-op outside herdr)
	defer herdr.Begin(e.herdrSession())()
	// flush the batched rollout fsync at the turn boundary so a completed Run is
	// durable even though per-message Append no longer fsyncs (see Rollout.Flush).
	if e.Sessions != nil {
		defer func() { _ = e.Sessions.Flush() }()
	}
	// usage and lifetime writes are queued off the turn path; land them before
	// the Run returns so a process exit right after loses nothing
	defer cost.Flush()
	defer e.cancelSpecCompact()
	defer e.earlyDrain()
	// reset per-Run state. freshRunState allocates the dedupe/streak maps;
	// sticky fields (sysPromptCache, warnedThinkingIgnored)
	// live on Engine and survive this assignment. The previous runState is
	// dropped — Run identity is the dedupe scope.
	e.run = e.freshRunState()
	e.gaugeArm("first")
	res := RunResult{}

	// probe the active model's context window so auto-compact knows the real
	// budget for novel models the hardcoded table doesn't carry (e.g. fresh
	// ollama pulls, deepseek-v4-pro, lm-studio custom configs). Best-effort;
	// dedupes per (provider,model) via probe.go; hosted lookups run detached.
	e.probeContextWindow(ctx)

	// `@path` expansion: inline file contents for any `@<rel>` token the
	// user typed. Applied to text blocks only; image blocks pass through.
	content = expandAtPathsInContent(content, e.Cwd)
	pl := e.planRun(ctx, collectUserText(content))

	// seed prior turns so multi-turn / resumed sessions have full context.
	// not re-persisted: caller owns disk state.
	res.Messages = append(res.Messages, e.InitialMessages...)

	// pre-compact: free budget BEFORE appending the new user turn so the
	// upcoming request has headroom. uses lastInputTokens from the prior
	// run when available; otherwise falls back to estimator over sys+history.
	if e.compactionEnabled() {
		budget, threshold := e.compactLimits()
		if ShouldAutoCompactWithUsage(pl.sys, res.Messages, e.run.lastInputTokens, budget, threshold) && compactWorthwhile(res.Messages, budget) {
			res.Messages = e.compactInline(ctx, res.Messages)
		}
	}

	// append user message; chain ParentID to last seeded msg when resuming
	userMessage := newMessage(types.RoleUser, lastID(res.Messages), content...)
	userMessage.Display = display
	if err := e.push(ctx, &res, userMessage); err != nil {
		return res, err
	}

	// state card mode: per-Run card replaces the rolling transcript on the
	// wire (see statecard.go). reset every Run so the goal tracks the latest
	// user ask; the session transcript itself is untouched.
	if e.Cfg.StateCard {
		e.run.card = newStateCard(e.cardGoalFor(pl.userText, e.InitialMessages))
	} else {
		e.run.card = nil
	}

	// resolve the per-Run iteration ceiling. 0 (or negative) = unlimited: the
	// loop runs until a real guard fires (token budget or read-only stall).
	// config.Defaults() seeds config.DefaultMaxIterations, so 0 only appears when the user explicitly
	// lifted the cap (/iterations 0 or max_iterations = 0).
	maxIter := e.Cfg.MaxIterations
	for i := 0; maxIter <= 0 || i < maxIter; i++ {
		// per iteration: a hosted window probe can land mid-Run
		tokenBudget := computeTokenBudget(e.Cfg)
		if err := e.handleBudgetCaps(ctx, &res.Messages, i, tokenBudget); err != nil {
			return res, err
		}
		e.drainSteer(ctx, &res)
		e.injectJobNotice(ctx, &res.Messages)

		// auto-compact when prompt+history nears the budget. Check at loop
		// top so newly-appended tool/assistant messages from prior iteration
		// are also accounted for. Prefer the provider's last reported input
		// token count (most accurate, works for every wire format that
		// surfaces usage); fall back to the estimator on the first turn. A
		// background summary started below the trigger makes the swap free.
		res.Messages = e.autoCompactStep(ctx, res.Messages, pl.sys)

		assistantMsg, finalText, toolUses, err := e.streamRecovering(ctx, pl, &res)
		if err != nil {
			return res, err
		}
		e.gaugeArm("between")
		assistantMsg.ParentID = lastID(res.Messages)
		if err := e.push(ctx, &res, assistantMsg); err != nil {
			return res, err
		}
		res.FinalText = finalText

		// cross-turn reasoning loop: fold this turn's reasoning into the
		// duplicate streak so injectIterAndTokenWarnings can fire a hard nudge
		// when the model rehashes the same thinking turn after turn.
		observeReasoningDup(e, assistantMsg.Content)

		// effort-off compliance: warn once if the model reasoned despite the
		// suppression signal (some templates ignore /no_think + enable_thinking).
		e.verifyThinkingSuppression(finalText, assistantMsg)

		nudge, err := e.settleTurn(assistantMsg, finalText, toolUses, pl, i == 0)
		if err != nil {
			return res, err
		}
		if nudge != nil {
			if err := e.push(ctx, &res, *nudge); err != nil {
				return res, err
			}
			continue
		}
		if len(toolUses) == 0 {
			return res, nil
		}
		if err := e.runTools(ctx, &res, assistantMsg.ID, toolUses, i+1, maxIter, tokenBudget, pl.readOnly); err != nil {
			return res, err
		}
		// a done signal alongside tool calls still dispatches them: returning
		// earlier would persist tool_use with no tool_result (model believes
		// it ran).
		if detectDoneSignal(finalText) {
			return res, nil
		}
	}
	// graceful wind-down: one tool-less turn so the user gets a close-out
	// summary rather than a bare cap error. best-effort, never blocks the exit.
	e.finalSummaryOnCap(ctx, &res, pl.sys, pl.sysDynamic)
	return res, &MaxIterationsError{Limit: maxIter}
}

// runTools dispatches a turn's tool calls and persists the tool message. a
// non-nil error ends the Run: cancellation, a repeat bail or an escalate, each
// raised only after the results are on the transcript.
func (e *Engine) runTools(ctx context.Context, res *RunResult, parentID string, uses []types.ToolUse, iter, maxIter, tokenBudget int, readOnly bool) error {
	// one-time confirmation that constrained json tool mode is live: the
	// first parsed call proves the grammar pipeline works end to end.
	if !e.jsonModeNoticeShown && strings.HasSuffix(e.Provider.Name(), "+jsonmode") {
		e.jsonModeNoticeShown = true
		e.noticef("json tool mode active · first tool call parsed clean")
	}

	// dispatch tools (read-only ones run in parallel, mutators serial)
	toolStart := time.Now()
	results, err := e.dispatchTools(ctx, uses)
	e.gaugeToolTime(time.Since(toolStart))
	if err != nil {
		// canceled mid-batch: persist what ran plus synthetic results so no
		// tool_use stays orphaned in the rollout. the turn ctx is dead, so
		// the append must not inherit its cancellation.
		_ = e.push(context.WithoutCancel(ctx), res, newMessage(types.RoleTool, parentID, toolResultBlocks(results)...))
		return err
	}
	if e.run.card != nil {
		e.run.card.observe(uses, results)
	}
	blocks := toolResultBlocks(results)
	blocks, repeatErr := observeRepeats(e, uses, results, blocks)
	blocks = observeEditNoVerify(e, uses, results, blocks)
	blocks = injectIterAndTokenWarnings(e, blocks, iter, maxIter, tokenBudget)
	blocks = append(blocks, toolImageBlocks(results)...)
	// a cancel landing while the last tool ran must not drop its results:
	// the assistant tool_use is already on disk, so the append ignores ctx.
	if err := e.push(context.WithoutCancel(ctx), res, newMessage(types.RoleTool, parentID, blocks...)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// two-strike bail: the result is appended first (so transcript shows what
	// failed) then the typed error goes to the caller.
	if repeatErr != nil {
		return repeatErr
	}
	// escalate bail: same shape — the synthetic tool_result is appended first
	// (transcript shows the model's reason) then ErrEscalate surfaces.
	if esc := e.run.escalateErr; esc != nil {
		e.run.escalateErr = nil
		return &EscalateError{Reason: esc.Reason, NextAction: esc.NextAction, Options: esc.Options}
	}
	return nil
}
