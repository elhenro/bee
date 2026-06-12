package loop

import "github.com/elhenro/bee/internal/tools/escalate"

// runState is the per-Run scratch state. Reset at the top of every Run so
// cross-Run dedupes (warnings, nudges, streaks) fire once per Run, not once per
// session. Fields that MUST survive across Runs (sys-prompt cache, vision
// memoization, model-switch sentinels) stay on Engine directly — see the
// "deliberately not reset per Run" comments in turn.go.
//
// Three field groupings live here:
//
//  1. Dedupes (warned*/nudges): "have we fired this notice yet this Run?"
//     warned* are bools; nudges is a map keyed by name so adding a new nudge
//     doesn't grow the struct by a field.
//  2. Counters/streaks: numeric progress signals (iter/token/stall). Reset
//     because they describe a single Run's run-time behavior.
//  3. Per-Run trackers (repeats, dupWrites, editVerify, escalateErr, card,
//     allowedTools, lastReasoningSig): trackers or buffers that have a clear
//     "start of Run" lifecycle.
//
// Why a struct and not Engine fields: the reset block at the top of every Run
// (turn_run.go) shrinks from 25+ lines to one. The lifecycle is type-expressed:
// `e.run` is nil-able for headless callers that don't loop, and tests can
// construct an Engine without remembering which bools to zero.
type runState struct {
	// allowedTools is the set of tool names the current Run actually advertised
	// after role/posture filtering. The executor gates on it so a model that
	// calls an unadvertised tool (e.g. a local model emitting write on a
	// read-only scout turn) is rejected instead of silently mutating. nil = no
	// gate (allow all registered).
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
	// nudges dedupes the warning prefixes that fire at most once per Run.
	// Keys: "two-strike", "repeat", "per-tool-fail", "same-result". Replaces
	// the per-nudge bool fields so adding a new nudge doesn't grow the struct.
	nudges map[string]bool
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
	// card is the per-Run state card when the active profile opts in
	// (state_card = true). nil = transcript mode (default). See statecard.go.
	card *stateCard
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
	// visionWarned dedupes the "no fallback configured" notice. Session-level:
	// the user fixing the config is a session decision, not a per-Run one.
	// freshRunState copies it forward from the previous runState so it
	// survives the per-Run reset.
	visionWarned bool
	// visionCache memoizes image-description text by content hash. Also
	// session-level — re-describing the same image across Runs is wasted
	// work. freshRunState carries the map forward by reference.
	visionCache map[string]string
}

// nudge keys for runState.nudges. Constants so call sites don't typo strings
// and the dedupe table is searchable.
const (
	nudgeTwoStrike    = "two-strike"
	nudgeRepeat       = "repeat"
	nudgePerToolFail  = "per-tool-fail"
	nudgeSameResult   = "same-result"
)

// freshRunState builds a zeroed per-Run state. All map fields are allocated
// so callers can read+write without nil-checks. Sticky fields (vision cache,
// warnedThinkingIgnored, jsonModeNoticeShown, sysPromptCache, profileScaledFor)
// stay on Engine and are NOT touched here.
//
// Session-level memoization that lives on runState (visionCache, visionWarned,
// lastReasoningSig) is borrowed from the previous runState by reference where
// possible and by value where not, so the new runState starts with the prior
// session's state preserved.
func (e *Engine) freshRunState() *runState {
	rs := &runState{
		allowedTools:       make(map[string]bool),
		editsByFile:        make(map[string]int),
		nudgedEditNoVerify: make(map[string]bool),
		nudges:             make(map[string]bool),
	}
	// visionCache: keep the session-warm map. allocated fresh if this is the
	// first Run.
	if e.run != nil {
		rs.visionCache = e.run.visionCache
		rs.visionWarned = e.run.visionWarned
	} else {
		rs.visionCache = map[string]string{}
	}
	// lastReasoningSig is also session-level — turn_run.go copies it forward
	// from the prior runState after freshRunState returns.
	return rs
}

// nudgeOnce records that a one-time nudge fired this Run. Returns true if
// the nudge should actually run (i.e. it hasn't fired yet). The boolean return
// is the dedupe gate; callers wrap their nudge logic in `if nudgeOnce(...) { ... }`.
func (e *Engine) nudgeOnce(key string) bool {
	if e.run.nudges[key] {
		return false
	}
	e.run.nudges[key] = true
	return true
}
