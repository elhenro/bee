// Package loop drives one bee turn: build prompt, stream provider,
// dispatch tools, persist to rollout, recurse until the model stops.
package loop

import (
	"context"
	"io"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
	"github.com/elhenro/bee/internal/waggle"
)

// MaxIterations is the default safety cap: if the model keeps emitting
// tool_use past this many turns, abort. Per-run override via
// Config.MaxIterations (0 = unlimited).
const MaxIterations = config.DefaultMaxIterations

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
	// Waggle, when non-nil, observes read-only tool calls so repeated routes can
	// be crystallized into reusable exec-skills (procedure memory). nil disables.
	Waggle *waggle.Manager
	// Replay, when non-nil, follows previously crystallized routes: after a tool
	// batch it matches the recent read-only calls against stored waggle prefixes
	// and, on a confident match, runs the route's remaining read-only steps off
	// the model's path, folding their output into the triggering tool result so
	// the model skips the round-trips. Zero prompt cost (matching is in Go). nil
	// disables.
	Replay *waggle.Replayer
	Cfg    config.Config
	Cwd    string
	Stdout io.Writer
	// SteerCh, when non-nil, is drained at the top of each iteration to
	// inject mid-turn user steering between LLM rounds.
	SteerCh chan string
	// StreamCh, when non-nil, receives every text delta produced by the
	// provider in lieu of writing them to Stdout. The TUI uses this to
	// route deltas through bubbletea so the alt-screen isn't corrupted.
	// Sends are non-blocking — a slow consumer drops deltas rather than
	// stalling the model stream.
	StreamCh chan string
	// ThinkCh, when non-nil, receives every chain-of-thought delta as it
	// arrives. Separate from StreamCh so the TUI can render reasoning
	// live in a dimmed/italic block above the answer instead of waiting
	// for the whole thinking buffer to flush at end-of-stream. Sends are
	// non-blocking — slow consumer drops deltas.
	ThinkCh chan string
	// ProgressCh, when non-nil, receives the char count of text the provider
	// generated but withheld from StreamCh (buffered tool-call modes parse
	// the whole body before emitting). Keeps the TUI's live output figure
	// moving when no displayable deltas flow. Sends are non-blocking.
	ProgressCh chan int
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

	// ToolsForCwd, when non-nil, builds a fresh tool registry rooted at a given
	// directory. Queen mode uses it to point an isolated worker engine at its
	// own git worktree so file tools (read/write/grep) target the worktree, not
	// the shared cwd. nil = no isolation available (workers stay on the shared
	// tree). Set by the TUI/CLI where the registry builder lives.
	ToolsForCwd func(cwd string) (*tools.Registry, error)

	// OnceAllowTools force-allows plan-only tools (e.g. ask_user) for the next
	// Run, regardless of the active role. A prompt skill sets it from its
	// frontmatter `tools` list so /plan can ask the user even from worker.
	// Only plan-only tools are honoured — it can't re-enable write/bash that a
	// read-only turn legitimately strips. Cleared at the top of each Run.
	OnceAllowTools []string

	// run holds the per-Run scratch state (dedupes, streaks, trackers). Reset
	// to a fresh runState at the top of every Run; the previous map values are
	// dropped, not preserved (Run identity is what defines the dedupe scope).
	// nil before the first Run — callers that never invoke Run (headless tests,
	// hive workers that bypass the loop) leave it nil.
	run *runState

	// jsonModeNoticeShown dedupes the one-time "json tool mode active"
	// confirmation per session. Deliberately NOT reset per Run.
	jsonModeNoticeShown bool
	// sysPromptCache memoizes Assemble output across Runs. key is a cheap
	// digest of mode/profile + spec/skill/recs/ctxFile fingerprints. dynamic is
	// the volatile memory tail of value (for the prompt-cache breakpoint split).
	sysPromptCache struct {
		key     string
		value   string
		dynamic string
	}
	// profileScaled tracks whether the tiny-profile budget was already widened
	// for the active model's context window. Sticky: scaling is idempotent
	// for a given (model, ctx) pair, and we re-scale on model switch via the
	// model-id check.
	profileScaledFor string
	// warnedThinkingIgnored dedupes the "model ignores thinking suppression"
	// notice. session-level (NOT reset per Run): the model rarely changes
	// mid-session, so one warning is enough.
	warnedThinkingIgnored bool
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
