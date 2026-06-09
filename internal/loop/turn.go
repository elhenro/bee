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
	"github.com/elhenro/bee/internal/tools/escalate"
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

	// SkipPostureClassifier disables the worker read-only/act classifier for
	// this engine, so a worker turn always gets the full tool surface. Set for
	// the scripted-provider test harness (the extra side Stream call would
	// desync scripted response counts) and for callers that want byte-for-byte
	// "always act" worker behavior.
	SkipPostureClassifier bool

	// allowedTools is the set of tool names the current Run actually advertised
	// after role/posture filtering. The executor gates on it so a model that
	// calls an unadvertised tool (e.g. a local model emitting write on a
	// read-only scout turn) is rejected instead of silently mutating. Rebuilt
	// each Run from the filtered specs; nil = no gate (allow all registered).
	allowedTools map[string]bool

	// lastInputTokens is the most recent provider-reported input-token count
	// from the latest EventDone usage. Used to drive the context-window
	// warning injection. Reset at the top of each Run.
	lastInputTokens int
	// warnedContext flips true once the context-warning prefix has been
	// injected into a tool result this Run. dedupes — caller sees one notice.
	warnedContext bool
	// iteration progress / stall tracking; reset per Run.
	warnedIterHalf      bool
	warnedIterEighty    bool
	warnedStall         bool
	warnedStallEscalate bool
	noMutationStreak    int
	// editsByFile counts edits per path since the last verify (build/test
	// run or read of the same path). Resets per Run.
	editsByFile map[string]int
	// nudgedEditNoVerify dedupes the per-file edit-no-verify nudge so the
	// model isn't spammed every iter once threshold crossed.
	nudgedEditNoVerify map[string]bool
	// cumulative token spend across iterations of one Run. drives the
	// adaptive token-budget cap so long productive turns aren't bounded
	// purely by iter count. reset per Run.
	cumInputTokens  int
	cumOutputTokens int
	// warnedTokenHalf / Eighty: token-budget warnings dedupe per Run.
	warnedTokenHalf   bool
	warnedTokenEighty bool
	// budgetRecoveries counts how many times the token-budget cap was hit and
	// auto-recovered (compact + re-arm) this Run, instead of hard-stopping.
	// bounds total spend at ~(maxBudgetRecoveries+1)×budget. reset per Run.
	budgetRecoveries int
	// nudgedReasoningOnly flips true after one synthetic continuation nudge
	// is injected in response to a thinking-only assistant turn. dedupes per
	// Run so a wedged provider can't burn the whole iter budget.
	nudgedReasoningOnly bool
	// formatNudgeCount counts how many format-correction nudges have fired
	// this Run. Allows up to formatNudgeMax retries with escalating wording
	// before format-strike bail fires. separate from reasoning-only dedupe
	// because the two failure modes need independent budgets.
	formatNudgeCount int
	// formatSlipStreak counts consecutive turns where the assistant produced
	// no tool_use but the text looked like a malformed envelope. Reset by any
	// turn that dispatches a tool. Drives FormatStrikeError at formatStrikeAt.
	formatSlipStreak int
	// repeats tracks tool-call signatures across iterations of one Run so
	// the loop can detect identical-call loops, per-tool failure cascades,
	// and two-strike escalations. allocated lazily on first dispatch.
	repeats *repeatTracker
	// nudgedRepeat / nudgedPerToolFail / nudgedTwoStrike dedupe the
	// corresponding warning prefixes — each fires at most once per Run.
	nudgedRepeat      bool
	nudgedPerToolFail bool
	nudgedTwoStrike   bool
	// lastTurnLooped flags that the just-finished stream was cut mid-repetition
	// so the turn loop injects a corrective nudge instead of treating the
	// partial text as a clean finish. read and cleared each iteration.
	lastTurnLooped bool
	// loopCutStreak counts consecutive turns cut for degenerate repetition.
	// drives RepeatStreamError at loopCutBailAt; reset by any clean stream.
	loopCutStreak int
	// lastReasoningSig fingerprints the prior turn's reasoning/text. when N
	// consecutive turns rehash near-identical reasoning the model is spinning
	// across turn boundaries (each turn under the in-stream cut threshold), so
	// reasoningDupStreak drives a hard escalate nudge. reset by a turn whose
	// reasoning diverges enough.
	lastReasoningSig   map[string]struct{}
	reasoningDupStreak int
	warnedReasoningDup bool
	// lastTurnTruncated flags that the just-finished stream dropped mid-output
	// on a transient error after content streamed. the turn loop keeps the
	// partial turn and nudges to continue instead of failing the Run.
	lastTurnTruncated bool
	// truncCutStreak counts consecutive turns that dropped mid-output with no
	// progress. drives TruncatedStreamError at truncCutBailAt; reset by progress.
	truncCutStreak int
	// dupWrites tracks (path, content-hash) of writes within one Run so the
	// engine can warn on duplicate identical writes. opt-in per profile.
	dupWrites *duplicateWriteTracker
	// escalateErr stashes the escalate-tool payload during dispatch so
	// dispatchTools can return ErrEscalate after the synthetic tool_result
	// lands in the transcript. nil = no escalation in flight.
	escalateErr *escalate.Error
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
	// visionCache memoizes image-description text by content hash so a
	// non-vision main model doesn't re-describe the same image every turn.
	// visionWarned dedupes the "no fallback configured" notice per Run.
	visionCache  map[string]string
	visionWarned bool
	// emptyCompletionStreak counts consecutive turns that produced no text, no
	// reasoning, and no tool call (whitespace-only output). drives
	// EmptyCompletionError at emptyCompletionBailAt; reset by any turn with
	// content. catches models whose thinking-suppression switch degenerates the
	// response to whitespace instead of suppressing the trace.
	emptyCompletionStreak int
	// thinkingSuppressRequested records whether this turn asked the model to
	// skip reasoning (effort off on a model that nominally supports thinking).
	// set per turn; read by verifyThinkingSuppression to detect non-compliance.
	thinkingSuppressRequested bool
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
