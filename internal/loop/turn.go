// Package loop drives one bee turn: build prompt, stream provider,
// dispatch tools, persist to rollout, recurse until the model stops.
package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// MaxIterations is the default safety cap: if the model keeps emitting
// tool_use past this many turns, abort. Override per-engine via
// Config.MaxIterations (0 = use this default).
const MaxIterations = 50

// KnowledgeStore abstracts knowledge selection so the engine doesn't pull
// in the full knowledge package (and tests can stub it).
type KnowledgeStore interface {
	Query(ctx context.Context, query string, recentTools []string) ([]knowledge.Record, error)
}

// Engine wires every component bee needs to run one or many turns.
type Engine struct {
	Provider llm.Provider
	Tools    *tools.Registry
	Skills   *skills.Registry
	Memory   KnowledgeStore
	Sessions *session.Rollout
	Cfg      config.Config
	Cwd      string
	Stdout   io.Writer
	// SteerCh, when non-nil, is drained at the top of each iteration to
	// inject mid-turn user steering between LLM rounds.
	SteerCh chan string
	// StreamCh, when non-nil, receives every text delta produced by the
	// provider in lieu of writing them to Stdout. The TUI uses this to
	// route deltas through bubbletea so the alt-screen isn't corrupted.
	// Sends are non-blocking — a slow consumer drops deltas rather than
	// stalling the model stream.
	StreamCh chan string
	// LiveMsgCh, when non-nil, receives every assistant + tool message as
	// it's persisted, so a UI can render tool_use / tool_result cards in
	// real time instead of only after Run returns. User messages are NOT
	// sent (the caller's UI already shows an optimistic copy). Sends are
	// non-blocking — a stalled consumer doesn't stall the loop.
	LiveMsgCh chan types.Message
	// WarnCh, when non-nil, receives transient operational notices: stream
	// retries, watchdog stalls, etc. The TUI fades them as a small chrome
	// line so the user knows something happened without the turn aborting.
	// Sends are non-blocking — a slow consumer drops notices.
	WarnCh chan string
	// JSONEmitter, when non-nil, receives one NDJSON event per significant
	// happening (text delta, tool use, tool result, done, error) and
	// suppresses the human-readable text-delta write to Stdout.
	JSONEmitter *jsonmode.Emitter
	// Costs, when non-nil, accumulates per-turn usage/dollar events. The
	// TUI reads from the same tracker to drive the top-bar total and the
	// /cost monitor pane.
	Costs *cost.Tracker
	// InitialMessages, when non-nil, seeds the in-memory message list at
	// the start of each Run so the model receives prior turns as context.
	// The TUI refreshes this per submit; `bee back <id>` sets it from disk.
	// Messages here are NOT re-appended to the rollout — they're already on
	// disk or never were (caller's responsibility).
	InitialMessages []types.Message
	// Rebuild, when non-nil, is invoked by the TUI after a mid-session
	// provider/model switch (`/model` or the picker). The closure owns
	// re-creating Provider + Memory from the current Cfg so the next turn
	// talks to the new backend instead of the original one cached at Engine
	// construction. nil = no live switching (headless, hive workers).
	Rebuild func(*Engine) error

	// lastInputTokens is the most recent provider-reported input-token count
	// from the latest EventDone usage. Used to drive the context-window
	// warning injection. Reset at the top of each Run.
	lastInputTokens int
	// warnedContext flips true once the context-warning prefix has been
	// injected into a tool result this Run. dedupes — caller sees one notice.
	warnedContext bool
	// iteration progress / stall tracking; reset per Run.
	warnedIterHalf bool
	warnedIterEighty bool
	warnedStall      bool
	noMutationStreak int
	// nudgedReasoningOnly flips true after one synthetic continuation nudge
	// is injected in response to a thinking-only assistant turn. dedupes per
	// Run so a wedged provider can't burn the whole iter budget.
	nudgedReasoningOnly bool
}

// mutatorTools are names that count as state-changing for stall detection.
// when none of these run for a long streak, the model is probably stuck
// in explore-loop; we nudge it.
var mutatorTools = map[string]bool{
	"bash":          true,
	"apply_patch":   true,
	"edit":          true,
	"hashline_edit": true,
	"write":         true,
}

// RunResult is the aggregate produced by one Run call.
type RunResult struct {
	Messages  []types.Message
	FinalText string
}

// Run executes the agent loop until the model emits a stop without tool use,
// or MaxIterations is hit. The user message is appended to the session.
// Thin wrapper around RunWithContent for the text-only call path.
func (e *Engine) Run(ctx context.Context, userMsg string) (RunResult, error) {
	return e.RunWithContent(ctx, []types.ContentBlock{{Type: types.BlockText, Text: userMsg}})
}

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
	// trim tool surface for tiny-profile models — see filterToolSpecsForProfile
	specs = filterToolSpecsForProfile(specs, e.Cfg.Profile, e.Cfg.ExtraTools...)
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
				if compacted, cerr := Compact(ctx, e.Provider, e.Cfg.DefaultModel, res.Messages); cerr == nil {
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
			Thinking: llm.ParseThinking(e.Cfg.Thinking),
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

// maxPreContentRetries caps the reopen budget when the provider fails before
// emitting any content. Beyond this we surface the error rather than risk a
// stuck retry loop.
const maxPreContentRetries = 2

// preContentRetryDelay is the gap before re-opening the stream after a
// pre-content failure. Var, not const, so tests can shrink it.
var preContentRetryDelay = 800 * time.Millisecond

// streamOnce drains one provider stream into a single assistant message.
// On pre-content transient errors it reopens up to maxPreContentRetries times
// and emits a WarnCh notice per retry.
func (e *Engine) streamOnce(ctx context.Context, req llm.Request) (types.Message, string, []types.ToolUse, error) {
	var (
		textBuf  strings.Builder
		thinkBuf strings.Builder
		content  []types.ContentBlock
		toolUses []types.ToolUse
	)
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return types.Message{}, "", nil, ctx.Err()
			case <-time.After(preContentRetryDelay):
			}
		}
		msg, finalText, uses, gotContent, retry, err := e.streamAttempt(ctx, req, &textBuf, &thinkBuf, &content, &toolUses)
		if retry && !gotContent && attempt < maxPreContentRetries {
			e.warnf("stream hiccup (%v) — retrying %d/%d", err, attempt+1, maxPreContentRetries)
			textBuf.Reset()
			thinkBuf.Reset()
			content = content[:0]
			toolUses = toolUses[:0]
			continue
		}
		return msg, finalText, uses, err
	}
}

// warnf sends a transient warning to WarnCh if wired. Non-blocking — a slow
// consumer drops the notice rather than stalling the loop.
func (e *Engine) warnf(format string, args ...any) {
	if e == nil || e.WarnCh == nil {
		return
	}
	select {
	case e.WarnCh <- fmt.Sprintf(format, args...):
	default:
	}
}

// streamAttempt runs one Provider.Stream pass into the supplied buffers.
// Returns (msg, finalText, toolUses, gotContent, retryable, err). When
// retryable is true and gotContent is false, the caller may reopen the stream.
func (e *Engine) streamAttempt(
	ctx context.Context,
	req llm.Request,
	textBuf, thinkBuf *strings.Builder,
	content *[]types.ContentBlock,
	toolUses *[]types.ToolUse,
) (types.Message, string, []types.ToolUse, bool, bool, error) {
	ch, err := e.Provider.Stream(ctx, req)
	if err != nil {
		// pre-stream HTTP errors already exhaust the provider's own retry
		// budget — surface as terminal, no further retry.
		return types.Message{}, "", nil, false, false, fmt.Errorf("provider stream: %w", err)
	}
	gotContent := false
	for ev := range ch {
		if ctx.Err() != nil {
			return types.Message{}, "", nil, false, false, ctx.Err()
		}
		switch ev.Type {
		case llm.EventThinkingDelta:
			thinkBuf.WriteString(ev.Delta)
			gotContent = true
			if e.JSONEmitter != nil {
				e.JSONEmitter.Emit(jsonmode.Event{Type: "thinking", Delta: ev.Delta})
			}
		case llm.EventTextDelta:
			textBuf.WriteString(ev.Delta)
			gotContent = true
			if e.JSONEmitter != nil {
				e.JSONEmitter.Emit(jsonmode.Event{Type: "text", Delta: ev.Delta})
			} else if e.StreamCh != nil {
				select {
				case e.StreamCh <- ev.Delta:
				default:
				}
			} else {
				_, _ = e.Stdout.Write([]byte(ev.Delta))
			}
		case llm.EventToolUse:
			if ev.ToolUse != nil {
				*toolUses = append(*toolUses, *ev.ToolUse)
				gotContent = true
				if e.JSONEmitter != nil {
					e.JSONEmitter.Emit(jsonmode.Event{
						Type:  "tool_use",
						Name:  ev.ToolUse.Name,
						UseID: ev.ToolUse.ID,
						Input: ev.ToolUse.Input,
					})
				}
			}
		case llm.EventError:
			if ev.Err != nil {
				// drain remaining events so the provider goroutine exits cleanly
				for range ch {
				}
				if !gotContent && isTransientStreamErr(ev.Err) {
					return types.Message{}, "", nil, false, true, ev.Err
				}
				if e.JSONEmitter != nil {
					e.JSONEmitter.Emit(jsonmode.Event{Type: "error", Message: ev.Err.Error()})
				}
				return types.Message{}, "", nil, gotContent, false, ev.Err
			}
		case llm.EventDone:
			if e.JSONEmitter != nil {
				u := &jsonmode.Usage{}
				if ev.Usage != nil {
					u.Input = ev.Usage.InputTokens
					u.Output = ev.Usage.OutputTokens
				}
				e.JSONEmitter.Emit(jsonmode.Event{Type: "done", Usage: u})
			}
			if e.Costs != nil && ev.Usage != nil {
				e.Costs.Record(e.Cfg.DefaultProvider, req.Model, ev.Usage.InputTokens, ev.Usage.OutputTokens)
			}
			if ev.Usage != nil && ev.Usage.InputTokens > 0 {
				e.lastInputTokens = ev.Usage.InputTokens
			}
		}
	}
	// thinking block first so the rendered transcript reads in causal order
	if th := thinkBuf.String(); th != "" {
		*content = append(*content, types.ContentBlock{Type: types.BlockThinking, Text: th})
	}
	if t := textBuf.String(); t != "" {
		*content = append(*content, types.ContentBlock{Type: types.BlockText, Text: t})
	}
	for i := range *toolUses {
		tu := (*toolUses)[i]
		*content = append(*content, types.ContentBlock{Type: types.BlockToolUse, Use: &tu})
	}
	msg := types.Message{
		ID:      newID(),
		Role:    types.RoleAssistant,
		Content: *content,
		Time:    time.Now().UTC(),
	}
	return msg, textBuf.String(), *toolUses, gotContent, false, nil
}

// isTransientStreamErr returns true for momentary network / provider hiccups.
// Safe to retry only before any content was emitted — caller's responsibility.
func isTransientStreamErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, m := range []string{
		"sse scan",
		"stream stalled",
		"context deadline",
		"Client.Timeout",
		"EOF",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"use of closed network",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// profileToolAllowlist trims the tool surface advertised per profile. Registry
// stays full so any explicit call still executes, but the manifest + request
// schema only carry the listed tools. Pi-aligned defaults: tiny converges on
// {bash,read,write,edit}; normal on the pi 7-tool surface; large adds the
// expert-mode patch tools.
//
//   - tiny: 4-tool minimum for 4k-ctx models. No grep/find — bash covers them.
//   - normal: pi-shaped 7-tool surface (bash, read, write, edit, grep, find, ls).
//   - large: full surface incl. apply_patch + hashline_edit for capable models.
//
// A profile absent from this map passes through unfiltered.
var profileToolAllowlist = map[string]map[string]bool{
	"tiny": {
		"bash":  true,
		"read":  true,
		"write": true,
		"edit":  true,
	},
	"normal": {
		"bash":          true,
		"read":          true,
		"write":         true,
		"edit":          true,
		"grep":          true,
		"find":          true,
		"ls":            true,
		"knowledge_search": true,
	},
}

// filterToolSpecsForProfile drops tool specs that fall outside the profile's
// allowlist. Profiles with no allowlist (e.g. "large") pass through. Names
// in extras are force-allowed regardless of profile — the opt-in hatch for
// power tools like apply_patch / hashline_edit when staying on a small
// profile.
func filterToolSpecsForProfile(specs []llm.ToolSpec, profile string, extras ...string) []llm.ToolSpec {
	allow, ok := profileToolAllowlist[profile]
	if !ok {
		return specs
	}
	extra := make(map[string]bool, len(extras))
	for _, n := range extras {
		extra[n] = true
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, s := range specs {
		if allow[s.Name] || extra[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// safeParallelTools lists read-only tools that can run concurrently within
// one turn. Mutators (shell, apply_patch, edit_diff, hashline_edit, write)
// stay serial to preserve happens-before and avoid sandbox contention.
var safeParallelTools = map[string]bool{
	"read":          true,
	"grep":          true,
	"find":          true,
	"ls":            true,
	"knowledge_search": true,
}

// dispatchTools runs read-only tools concurrently and mutators serially.
// Order in the returned slice matches the input order so UseIDs line up
// with the original ToolUse blocks. A serial tool acts as a barrier:
// all in-flight parallel tools complete before it runs, and nothing starts
// after it until it finishes. ctx cancellation short-circuits.
func (e *Engine) dispatchTools(ctx context.Context, uses []types.ToolUse) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, len(uses))
	var wg sync.WaitGroup
	flush := func() { wg.Wait() }
	for i, u := range uses {
		if ctx.Err() != nil {
			flush()
			return results, ctx.Err()
		}
		if safeParallelTools[u.Name] {
			wg.Add(1)
			go func(idx int, use types.ToolUse) {
				defer wg.Done()
				results[idx] = e.runOneTrapped(ctx, use)
			}(i, u)
			continue
		}
		// mutator / shell — drain pending parallel work first, run serially
		flush()
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		results[i] = e.runOneTrapped(ctx, u)
	}
	flush()
	for _, r := range results {
		if e.JSONEmitter != nil {
			e.JSONEmitter.Emit(jsonmode.Event{
				Type:    "tool_result",
				UseID:   r.UseID,
				Content: r.Content,
				Error:   r.IsError,
			})
		}
	}
	return results, nil
}

// runOneTrapped wraps runOne so ctx cancel propagates while ordinary tool
// errors are folded into a ToolResult the model can react to. Used by the
// parallel dispatcher where caller can't directly return an err.
func (e *Engine) runOneTrapped(ctx context.Context, u types.ToolUse) types.ToolResult {
	res, err := e.runOne(ctx, u)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return types.ToolResult{UseID: u.ID, Content: err.Error(), IsError: true}
		}
		return types.ToolResult{
			UseID:   u.ID,
			Content: fmt.Sprintf("tool error: %v", err),
			IsError: true,
		}
	}
	return res
}

func (e *Engine) runOne(ctx context.Context, u types.ToolUse) (types.ToolResult, error) {
	if e.Tools == nil {
		return types.ToolResult{UseID: u.ID, Content: "no tools registered", IsError: true}, nil
	}
	t, ok := e.Tools.Get(u.Name)
	if !ok {
		return types.ToolResult{UseID: u.ID, Content: fmt.Sprintf("unknown tool %q", u.Name), IsError: true}, nil
	}
	input := u.Input
	if u.Name == "bash" {
		input = wrapShellInput(input, e.Cfg.Sandbox, e.Cwd)
	}
	out, err := t.Run(ctx, input)
	if err != nil {
		return types.ToolResult{}, err
	}
	// scrub obvious secrets before fold into model context. defense layer:
	// shell stdout / read output can carry env files, key dumps, etc.
	out.Content = safety.Redact(out.Content)
	content, truncated := tools.TruncateWithLimit(u.Name, out.Content, config.ActiveProfile(e.Cfg).ToolOutputTokens)
	if truncated && e.JSONEmitter != nil {
		e.JSONEmitter.Emit(jsonmode.Event{Type: "tool_truncated", Name: u.Name, UseID: u.ID})
	}
	return types.ToolResult{UseID: u.ID, Content: content, IsError: out.IsError}, nil
}

// prependWarningToToolResult injects a context-warning prefix into the first
// tool_result block so the model sees it in the next turn. Mutates the
// ToolResult.Content via the pointer carried in the block. Safe when blocks
// is empty (no-op).
func prependWarningToToolResult(blocks []types.ContentBlock, warning string) []types.ContentBlock {
	for i := range blocks {
		if blocks[i].Type == types.BlockToolResult && blocks[i].Result != nil {
			blocks[i].Result.Content = warning + blocks[i].Result.Content
			return blocks
		}
	}
	return blocks
}

// toolResultBlocks renders results as ContentBlock list for a tool message.
func toolResultBlocks(rs []types.ToolResult) []types.ContentBlock {
	out := make([]types.ContentBlock, len(rs))
	for i := range rs {
		r := rs[i]
		out[i] = types.ContentBlock{Type: types.BlockToolResult, Result: &r}
	}
	return out
}

func (e *Engine) appendMessage(ctx context.Context, m types.Message) error {
	var err error
	if e.Sessions != nil {
		err = e.Sessions.Append(ctx, m)
	}
	// fan out to a live UI so tool_use / tool_result cards render mid-Run.
	// skip user role: the TUI shows an optimistic copy before Run starts.
	if e.LiveMsgCh != nil && m.Role != types.RoleUser {
		select {
		case e.LiveMsgCh <- m:
		default:
		}
	}
	return err
}

func lastID(ms []types.Message) string {
	if len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1].ID
}

func newID() string { return uuid.NewString() }

// hasThinkingOnly reports whether msg carries a thinking block but no text
// and no tool_use. provider produced reasoning then stopped — turn would
// otherwise terminate silently.
func hasThinkingOnly(msg types.Message) bool {
	sawThinking := false
	for _, b := range msg.Content {
		switch b.Type {
		case types.BlockThinking:
			sawThinking = true
		case types.BlockText:
			if strings.TrimSpace(b.Text) != "" {
				return false
			}
		case types.BlockToolUse:
			return false
		}
	}
	return sawThinking
}

// expandAtPathsInContent rewrites text blocks in-place with `@path` expansions.
// Image / tool blocks pass through untouched. Empty cwd disables expansion.
func expandAtPathsInContent(content []types.ContentBlock, cwd string) []types.ContentBlock {
	if cwd == "" {
		return content
	}
	for i, c := range content {
		if c.Type != types.BlockText || c.Text == "" {
			continue
		}
		content[i].Text = prompt.ExpandAtPaths(c.Text, cwd)
	}
	return content
}

// collectUserText concatenates the text blocks in a user message — used to
// build a memory-selection query from multimodal content.
func collectUserText(content []types.ContentBlock) string {
	var b strings.Builder
	for _, c := range content {
		if c.Type == types.BlockText {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// Compact replaces the session's older messages with a summary in-place.
// Used by the /compact slash command. Persisting compacted history requires
// a session-file rewrite which is out of scope for F4 — the call returns
// success and the next turn loop's in-memory slice is re-evaluated by
// ShouldAutoCompact / Compact. Known limitation: replayed sessions on disk
// still contain the full history.
func (e *Engine) Compact(ctx context.Context) error {
	if e.Sessions == nil {
		return nil
	}
	msgs, err := session.Read(e.Sessions.ID())
	if err != nil {
		return err
	}
	out, err := Compact(ctx, e.Provider, e.Cfg.DefaultModel, msgs)
	if err != nil {
		return err
	}
	_ = out
	return nil
}

// contextBudget returns the active model's real token window. Cache wins
// when populated (hardcoded table or live-learned via ProbeOllamaContext).
// For local providers the prewarm goroutine may not have answered yet on
// turn one — fall back to a 32k floor (matches ollama's default per-model
// allocation and avoids the bogus 14k-cap warnings the old SystemPromptBudget*4
// heuristic produced). Returns 0 for unknown remote models so callers treat
// it as "don't fire warnings".
func contextBudget(cfg config.Config) int {
	if cw := llm.ContextWindow(cfg.DefaultModel); cw > 0 {
		return cw
	}
	if config.IsLocalProvider(cfg.DefaultProvider) {
		return 32768
	}
	return 0
}
