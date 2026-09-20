# bee Remaster — Ticket Specifications

Every ticket is self-contained: objective and why, exact file scope, approach, acceptance criteria (checkbox = verifiable), and a verification command. IDs are stable. Do not renumber.

**Status field:** set to `todo | wip | review | done | blocked` and append a dated worklog line when you touch a ticket. Never delete prior worklog entries.

**Global rules for every ticket** (repeated in WORKFLOWS §2 so it is never a surprise):
- Pure Go, no CGo. No new dependency unless nothing in stdlib or the existing module graph fits.
- Files ≤300 lines. Split before you exceed it.
- No provider name-drops in code comments — describe behavior, not vendor.
- Tests are first-class; every behavioral ticket adds `_test.go` coverage.
- Do not call a real API from a unit test. Use `BEE_TEST_PROVIDER=stub` / `scripted`.
- Offline smoke must still pass: `BEE_TEST_PROVIDER=stub ./bee run --headless "anything"`.

Design reference (read-only, do not vendor): the minimax-code modules named in each ticket. Port behavior and invariants, never TypeScript structure.

---

## E1 — Loop resilience

### T101 · ABAB-cycle oscillation signal
- **status:** todo · **priority:** P1 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** T105
- **Why:** `repeat_detect.go` catches the same call twice in a row, but misses A/B/A/B oscillation — read→edit→read→edit that makes no progress. This is the dominant "spinning" shape in long agent turns.
- **Scope:** new `internal/loop/cycle_detect.go`; extend `internal/loop/repeat_detect.go`; test `internal/loop/cycle_detect_test.go`.
- **Approach:** Maintain a bounded ring buffer (length 4) of per-step "action batch keys" — a stable hash of the tuple of `(tool name, canonical args hash)` for all tool calls in one step, sorted. Emit an ABAB signal when `[a,b,a,b]` with `a != b`. Dedupe: one signal per distinct `(a,b)` episode; reset the episode key when the pattern breaks. Reuse the existing canonical-JSON hashing already used for `ToolCallSignature`.
- **AC:**
  - [ ] `[a,b,a,b]` for distinct `a,b` emits exactly one cycle signal.
  - [ ] `[a,b,a,b,a,b]` (one continuous episode) does not re-emit.
  - [ ] `[a,a,b,b]`, `[a,b,c,d]`, `[a,b,a,c]` emit nothing.
  - [ ] A single-tool-call step and a multi-tool-call step both produce a batch key deterministically (order-independent).
  - [ ] Buffer never grows past 4.
- **Verify:** `go test ./internal/loop/... -run 'TestABAB|TestCycle' && go build ./...`
- **Out of scope:** reminder text/message construction (that is T105/T106).

### T102 · Error-family clustering signal
- **status:** todo · **priority:** P1 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** T105
- **Why:** Today a failing tool with slightly different args each time never trips the repeat counter, yet the model is stuck on one error *family*. Clustering failures by normalized error shape catches the real loop.
- **Scope:** new `internal/loop/error_family.go`; extend `internal/loop/repeat_detect.go`; test `internal/loop/error_family_test.go`.
- **Approach:** Derive a family key per failing tool result: `(toolName, normalizedErrorKind)`. Normalize by stripping volatile substrings — absolute temp paths, line/col numbers, hex addresses, PIDs, timestamps, quoted file paths — then hashing. Count consecutive same-family failures; expose the streak on `Observation`.
- **AC:**
  - [ ] Two failures whose messages differ only by a temp path / line number / PID collide on one family key.
  - [ ] Genuinely different errors (compile error vs. permission denied) get different keys.
  - [ ] Streak resets on any success of that tool.
  - [ ] Normalization is deterministic across runs (no map-iteration order leak).
  - [ ] Fixture table covers ≥6 real error shapes (go build, go test, permission denied, command not found, no such file, JSON parse).
- **Verify:** `go test ./internal/loop/... -run TestErrorFamily && go build ./...`

### T103 · Verified-progress exemption
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** T101, T102 · **blocks:** T105
- **Why:** A repeated call that *changed state* is legitimate (polling a build, re-reading after an edit). Without an exemption the guard produces false positives and agents learn to ignore it.
- **Scope:** `internal/loop/turn_repeat.go`, `internal/loop/run_state.go`, new `internal/loop/progress.go`; tests.
- **Approach:** Introduce a per-step `ProgressView` describing whether a step is `verifiedProgress` (observable state changed), `continuousPolling` (same read-only call, unchanged output), or `reset` (a mutating tool succeeded, clearing streaks). Only non-progressing repeats are reminder-eligible. Mutation of any file or a successful write/edit/shell-with-side-effect clears the streak for that loop key.
- **AC:**
  - [ ] A successful mutator call resets repetition streaks for the affected loop key.
  - [ ] A read-only call returning byte-identical output repeatedly is flagged `continuousPolling`.
  - [ ] A read-only call returning *changed* output is treated as progress and is not reminder-eligible.
  - [ ] `verifiedProgress` is consulted before any reminder candidate is produced.
  - [ ] Existing `repeat_detect` tests still pass unchanged (backward compatible).
- **Verify:** `go test ./internal/loop/... -run 'TestProgress|TestRepeat' && go build ./...`

### T104 · Unchanged-output polling streak guard
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** T105
- **Why:** Agents poll a slow async result (`bg`, `agents`, background sessions) with unchanged output and burn turns. A dedicated streak + "you will be notified" reminder stops it.
- **Scope:** `internal/loop/repeat_detect.go` (extend existing `sameResult` tracking), new `internal/loop/polling.go`; test `internal/loop/polling_test.go`.
- **Approach:** Reuse the per-signature last-output hash. Track `unchangedOccurrences`. Expose it on `Observation`. Do not construct reminder prose here — expose the streak only.
- **AC:**
  - [ ] N consecutive identical-output calls for one signature increment one streak counter.
  - [ ] Changed output resets the streak to 0.
  - [ ] Different signatures track independently.
  - [ ] Counter is bounded (no unbounded growth for a long turn).
  - [ ] Existing `SameResultCount` behavior (if relied on elsewhere) is preserved or migrated with tests.
- **Verify:** `go test ./internal/loop/... -run TestPolling && go build ./...`

### T105 · Turn-local guard observer + reminder policy
- **status:** todo · **priority:** P1 · **size:** L · **owner:** builder
- **depends:** T103, T104 · **blocks:** T106
- **Why:** T101–T104 produce signals; without a single owner of *policy* (which signal wins, how many occurrences before a reminder, one-shot semantics) each signal grows its own ad-hoc reaction.
- **Scope:** new package `internal/loop/guard/` (`guard.go`, `signals.go`, `reminder.go`, `state.go`, `contracts.go`); wire from `internal/loop/turn_repeat.go` / `turn.go`; tests under `internal/loop/guard/`.
- **Approach:** A `Guard` observes each step and returns at most one reminder. Per-run state keyed by session+turn, deleted at turn end. Reminder priority order: `unchanged_progress_repeat` > `same_error_family` > `exact_action_repeat` > `polling_repeat`. Threshold default 3, configurable. Reserve-before-emit: once a reminder is emitted for a turn, do not emit a second (mirror the "reserve before attempting" discipline so a failed injection never retries). Include a `clearDetectionStreaks` entry point for callers that know progress happened.
- **AC:**
  - [ ] Exactly one reminder is produced per turn at most.
  - [ ] Higher-priority signal wins when several fire on the same step.
  - [ ] Threshold is configurable; default is 3.
  - [ ] State is deleted at turn end (no cross-turn leak; assert with a test that runs two turns).
  - [ ] Guard is pure Go with no provider/TUI import (import-cycle test).
  - [ ] Signals are also exposed as structured observations for logging, independent of reminder emission.
- **Verify:** `go test ./internal/loop/guard/... && go test ./internal/loop/... && go build ./...`

### T106 · Non-durable reminder hardening
- **status:** todo · **priority:** P1 · **size:** S · **owner:** builder
- **depends:** T105
- **Why:** A runtime loop reminder must never be memorized as a durable user preference. Getting this wrong silently poisons the knowledge store.
- **Scope:** `internal/loop/guard/reminder.go`; tests.
- **Approach:** Every guard reminder carries an explicit "current Turn only; do not save or generalize into memory/skills" clause. Encode it once in a helper so all reminder texts inherit it.
- **AC:**
  - [ ] Every reminder string emitted by the guard contains the non-durable clause.
  - [ ] Tests assert the clause is present for each signal kind.
  - [ ] Reminder text for the polling signal states the agent will be notified automatically.
  - [ ] A reminder rendered inside a fenced code block in the transcript is not mistaken for the clause (regression guard).
- **Verify:** `go test ./internal/loop/guard/... -run TestReminder && go build ./...`

---

## E2 — Context economics

### T201 · Token estimator interface
- **status:** todo · **priority:** P1 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** T202
- **Why:** Compaction triggers, dynamic budgets, and the status line all want a token number. bee has `estimateMessageTokens` buried in `compact.go`. Make it a named, swappable seam.
- **Scope:** new `internal/loop/token_estimator.go`; refactor `internal/loop/compact.go` to consume it; tests.
- **Approach:** Define `TokenEstimator` interface with `EstimateMessages([]types.Message) int` and `EstimateContext([]types.Message) int`. Provide a default heuristic implementation wrapping the existing estimator so current numbers are unchanged. Keep it provider-agnostic; a provider-specific counter can be added later without touching call sites.
- **AC:**
  - [ ] Estimator is an interface with the default implementation producing the same numbers as today for the existing test corpus.
  - [ ] All existing call sites go through the interface.
  - [ ] No behavior change: `compact` tests pass unmodified.
  - [ ] Estimator has no provider import (import-cycle test).
- **Verify:** `go test ./internal/loop/... && go build ./...`

### T202 · Dynamic max-output resolver
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** T201
- **Why:** bee sends a configured `MaxTokens` regardless of how full the context already is, so on a small local window the request can exceed the model's limit. The fix is to derive the output budget from remaining context.
- **Scope:** `internal/llm/` (new `budget.go`), `internal/loop/turn_run.go` / `turn_stream.go`; config `internal/config/types.go` (safety-margin knob); tests.
- **Approach:** `resolveDynamicMaxTokens(contextWindow, configuredMax, estimatedContext, safetyMargin)` returns `min(configuredMax, max(floor, contextWindow - estimatedContext - safetyMargin))`. When `contextWindow <= 0` (unknown model), return `configuredMax` unchanged. Clamp to a positive floor so the model is always given non-zero room. Log the effective value at debug level only — never in the transcript.
- **AC:**
  - [ ] Unknown context window ⇒ configured value passed through unchanged.
  - [ ] Tight remaining context ⇒ output budget shrinks proportionally.
  - [ ] Result is never ≤ 0 and never exceeds `configuredMax`.
  - [ ] `safetyMargin` is configurable with a sane default.
  - [ ] The resolver is pure and table-tested (≥8 cases incl. boundary values).
  - [ ] No provider wire type leaked upward; resolver lives above the wire layer.
- **Verify:** `go test ./internal/llm/... -run TestDynamicMaxTokens && go test ./internal/loop/... && go build ./...`

### T203 · Proactive compaction trigger
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** T202
- **Why:** Compaction should fire *before* the provider rejects the request, with a reserve for the response, not at a flat percentage.
- **Scope:** `internal/loop/context.go`, `internal/loop/turn_compact.go`, `internal/loop/context_warning.go`; tests.
- **Approach:** Compute `providerInputLimit = min(floor(contextWindow * inputRatio), contextWindow - reserve, contextWindow - effectiveOutput - safetyMargin)` and `automaticTriggerAt = min(providerInputLimit, max(1, contextWindow - proactiveReserve))` where `proactiveReserve = min(reserve*2, contextWindow/4)`. Use the estimator from T201 for current usage. Preserve existing behavior when the window is unknown.
- **AC:**
  - [ ] Trigger point is strictly below the provider input limit.
  - [ ] Unknown window ⇒ existing threshold behavior preserved.
  - [ ] Warning fires before the trigger; trigger fires before the cap.
  - [ ] Input ratio / reserve / margin are configurable with documented defaults.
  - [ ] Table tests cover small (4k), medium (32k), large (200k) windows.
- **Verify:** `go test ./internal/loop/... -run 'TestContext|TestCompactTrigger' && go build ./...`

### T204 · Safe cut-point selector
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** T203
- **Why:** `compact.go` summarizes everything except the last 4 messages. That can cut in the middle of a tool-use/tool-result pair, leaving the provider an invalid message sequence.
- **Scope:** `internal/loop/compact.go`, new `internal/loop/cutpoint.go`; tests.
- **Approach:** Walk backwards from the target index to the nearest *safe* boundary: never split an assistant tool-call from its tool results; never start the kept window on an orphan tool result. If no safe cut point exists, skip compaction (this is the reason `no_safe_cut_point` in T205).
- **AC:**
  - [ ] A cut that would orphan a tool result is rejected in favour of the nearest earlier boundary.
  - [ ] When no safe boundary exists, compaction is skipped rather than producing an invalid sequence.
  - [ ] `PreserveTail` semantics are preserved or explicitly superseded with a documented decision.
  - [ ] Round-trip test: compacted history still marshals to a valid provider request per wire format.
  - [ ] Test fixtures include interleaved multi-tool-call steps.
- **Verify:** `go test ./internal/loop/... -run 'TestCutPoint|TestCompact' && go build ./...`

### T205 · Typed compaction skip reasons + observer events
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** T204
- **Why:** Silent no-op compaction is undebuggable. Typed reasons turn "why did it not compact?" into an answerable question.
- **Scope:** `internal/loop/compact.go`, `internal/loop/compact_notice.go`; tests.
- **Approach:** Define a `SkipReason` enum: `disabled`, `not_enough_messages`, `below_threshold`, `no_safe_cut_point`, `deferred_by_hook`, `summary_failed`, `summary_empty`. Surface via an observer callback and the existing notice path. Keep user-facing text terse.
- **AC:**
  - [ ] Every early return in the compaction path carries a typed reason.
  - [ ] Reasons are asserted in tests for each branch.
  - [ ] Observer fires on trigger evaluation and on commit with before/after message counts.
  - [ ] A summary-generation failure degrades to a skip, not a fatal error.
- **Verify:** `go test ./internal/loop/... -run 'TestCompactReason|TestCompact' && go build ./...`

### T206 · Compaction deferral hook
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** T205, T502
- **Why:** Users/plugins may want to veto compaction at a moment when it would destroy context they need. The hook framework (E5) provides the seam; this ticket wires it.
- **Scope:** `internal/loop/compact.go`, `internal/hooks/` integration; tests.
- **Approach:** Add a `beforeCompaction` predicate consulted after a plan is selected. A veto returns `deferred_by_hook`. With no hooks configured the behavior must be identical to today.
- **AC:**
  - [ ] A vetoing hook defers compaction and yields the typed reason.
  - [ ] No hooks configured ⇒ identical behavior (regression test).
  - [ ] A hook that errors does not block compaction (fail-open, logged).
  - [ ] Composite test proves deferral then successful compaction on the next attempt.
- **Verify:** `go test ./internal/loop/... -run TestCompactDefer && go build ./...`

---

## E3 — Prompt & reminder architecture

### T301 · Reminder provider registry
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** — · **blocks:** T302, T304
- **Why:** Reminders are injected ad hoc across `turn_warnings.go`, `statecard.go`, `compact_notice.go`, `context_warning.go`. Budget-based truncation in `internal/prompt` cannot reason about them. A registry with tiers fixes both.
- **Scope:** new `internal/prompt/reminders/` (`registry.go`, `provider.go`, `types.go`); `internal/prompt/assemble.go`; tests.
- **Approach:** Providers register in order and each returns a block or nothing. Two tiers: `critical` (survives full-reminder-disabled mode) and `droppable` (truncated first under budget). Registry resolves the ordered chain per turn and hands the assembled blocks to the prompt assembler. No provider may do IO during assembly.
- **AC:**
  - [ ] Providers run in registration order; output is deterministic.
  - [ ] `critical` blocks survive when droppable reminders are disabled/truncated.
  - [ ] A provider returning empty contributes no whitespace or separator artifacts.
  - [ ] Registry is exercised with ≥3 synthetic providers in tests.
  - [ ] Existing prompt assembly tests pass; budget truncation still respects `SystemPromptBudget`.
  - [ ] No import cycle between `internal/prompt/reminders` and `internal/loop`.
- **Verify:** `go test ./internal/prompt/... && go build ./...`

### T302 · Migrate ad-hoc reminders to providers
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** T301
- **Why:** The registry is worthless if the old injection paths remain. This is the payoff ticket.
- **Scope:** `internal/loop/turn_warnings.go`, `internal/loop/statecard.go`, `internal/loop/compact_notice.go`, `internal/prompt/context_warning.go`; tests.
- **Approach:** Convert each existing reminder to a provider, classify it critical vs. droppable, and delete the bespoke injection. Keep rendered text byte-identical where a test asserts it, or update the assertion deliberately with a note in the worklog.
- **AC:**
  - [ ] Each migrated reminder is a registered provider with an explicit tier.
  - [ ] No reminder string is emitted from the old code path (grep proves it).
  - [ ] Byte-identical output where existing golden tests assert text; deliberate changes documented.
  - [ ] Ordering of reminders in the assembled prompt matches or improves on today's order.
  - [ ] Budget truncation can now drop a droppable reminder without dropping a critical one (test).
- **Verify:** `go test ./internal/... && go build ./...`

### T303 · Plan (todo) tool
- **status:** todo · **priority:** P2 · **size:** M · **owner:** builder
- **depends:** — · **blocks:** T304
- **Why:** bee has no first-class plan surface. A plan the model maintains is the cheapest way to keep multi-step work coherent — and it unlocks the stale-plan reminder (T304).
- **Scope:** new `internal/tools/plan/` (`plan.go`, `plan_test.go`); register in `cmd/bee/run_tools.go` (both `buildToolsWithApprover` and `buildToolsFilteredWithApprover`); add to `readOnlyTools` only if it must not mutate files (it should not — plan state is session-local).
- **Approach:** Model-callable tool that replaces the current plan with a list of `{text, status}` where status ∈ `pending|in_progress|completed|cancelled`. Store per-session. At most one `in_progress` at a time (reject or coerce). Return a compact rendered summary so the model sees its own plan.
- **AC:**
  - [ ] Tool spec is registered and visible via `tool_lookup` on both headless and TUI paths.
  - [ ] Empty list clears the plan.
  - [ ] More than one `in_progress` is rejected or coerced deterministically (documented).
  - [ ] Tool does not touch the filesystem.
  - [ ] `buildToolsFiltered` write-confined runs still include it.
  - [ ] Tests cover set/update/clear and the multi-in_progress case.
- **Verify:** `go test ./internal/tools/plan/... && go build ./... && BEE_TEST_PROVIDER=stub ./bee run --headless "list your tools"`

### T304 · Stale-plan reminder provider
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** T301, T303
- **Why:** A plan that is never revisited is noise. Track turns-since-update and remind — then stop nagging once nothing is active.
- **Scope:** new `internal/prompt/reminders/plan.go`; consumes plan state from T303; tests.
- **Approach:** Track per-session `active/completed/cancelled` counts and `lastUpdatedTurn`/`lastReminderTurn`. Emit a reminder when the plan has active items and has not been updated for N turns, at most once per N turns. Suppress entirely when there are no active items (mirror the minimax todo-state rule).
- **AC:**
  - [ ] Reminder fires when active items exist and N turns pass without an update.
  - [ ] Reminder is suppressed when all items are terminal.
  - [ ] At most one reminder per N turns (no per-turn nag).
  - [ ] Clearing the plan removes all state for the session.
  - [ ] Droppable tier — truncation under budget is allowed.
- **Verify:** `go test ./internal/prompt/... -run TestPlanReminder && go build ./...`

---

## E4 — Safety & permission

### T401 · Quote-aware shell tokenizer
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** — · **blocks:** T402, T404
- **Why:** Every downstream safety decision (whitelist, splitter, slow-scan) needs correct tokenization. Doing it once, well, de-risks the whole epic.
- **Scope:** new `internal/safety/shell_tokenize.go`; tests.
- **Approach:** Pure lexical scanner producing tokens with quoting context preserved (which bytes were quoted, single vs. double). Handle `\` escapes, single quotes (fully literal), double quotes (literal except expansions), and operator tokens (`&&`, `||`, `;`, `|`, redirections). No execution, no filesystem access, no process spawn. Deterministic.
- **AC:**
  - [ ] Quoted strings are single tokens with correct quote-kind recorded.
  - [ ] Escaped whitespace does not split a token.
  - [ ] Operators are distinct tokens from words.
  - [ ] Unterminated quotes are reported as a parse failure, not silently accepted.
  - [ ] Table tests ≥15 cases incl. nested/adjacent quoting (`"a"'b'c`).
  - [ ] No `os/exec`, no `os`, no filesystem import in the file (assert by review + no such imports).
- **Verify:** `go test ./internal/safety/... -run TestTokenize && go build ./...`

### T402 · Word-only command-sequence parser + splitter
- **status:** todo · **priority:** P1 · **size:** L · **owner:** builder
- **depends:** T401 · **blocks:** T403, T405
- **Why:** To auto-approve safely we must prove a script is a *plain sequence of bare commands*. Anything with substitution, redirection, assignment, or a subshell is unprovable and must fall through to asking.
- **Scope:** new `internal/safety/bash_ast.go`, `internal/safety/bash_split.go`; tests.
- **Approach:** Port the decision boundary (not the code): a script is "plain" iff every command is built only from bare words / quoted strings / numbers / quote-concatenations, joined only by side-effect-free operators. Reject the whole script (return not-plain) on: parentheses/subshells, redirections, background `&`, command substitution, parameter expansion, assignment prefixes, empty command positions, parse errors. Also implement `bash -lc "<script>"` extraction (exactly 3 argv tokens, `-lc`/`-c`, known shell basename). Be conservative: uncertainty ⇒ not plain.
- **AC:**
  - [ ] `bash -lc "ls -la && cat x"` extracts and parses into two commands.
  - [ ] Substitution, redirection, subshell, assignment prefix, and dangling operator each make the whole script non-plain.
  - [ ] Double-quoted content containing an expansion ⇒ non-plain; single-quoted equivalent ⇒ plain.
  - [ ] Non-`-lc` invocations and non-shell executables return not-plain.
  - [ ] Table of ≥25 accept/reject cases with rationale comments.
  - [ ] `bash_split.go` splits a plain sequence on top-level operators only.
- **Verify:** `go test ./internal/safety/... -run 'TestBashAST|TestSplit' && go build ./...`

### T403 · Read-only safe-command whitelist
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** T402 · **blocks:** T405
- **Why:** Read-only commands should never prompt. Today bee approximates this with pattern heuristics; a whitelist with option blacklists is precise and explainable.
- **Scope:** new `internal/safety/safe_command.go`; tests.
- **Approach:** Unconditional safe set (e.g. `cat cd cut echo expr false grep head id ls nl paste pwd rev seq stat tail tr true uname wc whoami` — final list reviewed at implementation). Conditional rules for commands that are read-only *unless* given a dangerous flag: `git`, `find`, `rg`, `sed`, `base64` etc. — blacklist the flag that turns each into a writer/executor. `isKnownSafe(argv)` also unwraps `bash -lc` and is true only when **every** command in the plain sequence is safe.
- **AC:**
  - [ ] Each unconditional command auto-approves.
  - [ ] Each conditional command auto-approves without its dangerous flag and does **not** with it (≥2 cases per command).
  - [ ] `bash -lc "ls && cat f"` is safe; `bash -lc "ls && rm f"` is not.
  - [ ] Non-plain scripts are never reported safe.
  - [ ] Executable basename resolution handles absolute and `./`-relative paths.
  - [ ] Table tests cover every whitelisted command.
- **Verify:** `go test ./internal/safety/... -run TestSafeCommand && go build ./...`

### T404 · Slow-command performance guard
- **status:** todo · **priority:** P2 · **size:** M · **owner:** builder
- **depends:** T401
- **Why:** An agent that runs an unbounded filesystem scan looks hung. This is a *performance* guard, not a safety one — the matched commands are legitimate, just unbounded.
- **Scope:** new `internal/safety/slow_command.go`; tests.
- **Approach:** Flag only the unmistakable "scan a top-level root with no depth bound" shapes: `find <huge-root>` / recursive grep over a huge root, with no `-maxdepth` and no explicit subdirectory. Huge roots matched by exact normalized path. Wrapper/env prefixes are unwrapped before matching. **Prefer a miss over a false positive** — anything narrower passes. Guidance string is embedded in the deny reason.
- **AC:**
  - [ ] `find /` and `find $HOME`-style bounded-root scans are flagged.
  - [ ] `find /var/log` and anything with `-maxdepth` are allowed.
  - [ ] Matching is pure lexical — no filesystem access, identical behavior cross-platform.
  - [ ] Wrapper prefixes (`sudo`, `env FOO=bar`) are unwrapped before matching.
  - [ ] ≥8 negative cases proving no false positive.
- **Verify:** `go test ./internal/safety/... -run TestSlowCommand && go build ./...`

### T405 · Permission checker registry + decision reducer
- **status:** todo · **priority:** P1 · **size:** L · **owner:** builder
- **depends:** T402, T403
- **Why:** One `DetectDangerous` call cannot express per-tool policy. A checker registry with a reject→allow→ask reducer makes policy composable and keeps existing safety as one checker among several.
- **Scope:** `internal/approval/` (new `engine.go`, `registry.go`), `internal/safety/` (wrap existing `DetectDangerous` as a checker); `internal/tools/shell/`; tests.
- **Approach:** Define `ToolPermissionChecker` (`check(toolName, input, ctx) -> allow|deny|passthrough`). Reducer order: (1) reject check — hard block, unaffected by bypass; (2) allow fast-path — e.g. T403 whitelist; (3) fallback — passthrough ⇒ ask. The shell checker composes T402/T403/T404. Keep `safety.DetectDangerous` as the fallback-deny checker so no protection is lost.
- **AC:**
  - [ ] Reducer order is fixed and unit-tested for each branch.
  - [ ] Bypass/`--yes` mode cannot override a hard reject.
  - [ ] A whitelisted read-only command never prompts.
  - [ ] A slow-command match yields deny with the guidance string as reason.
  - [ ] `DetectDangerous` behavior is preserved as a checker (its existing tests pass through the new path).
  - [ ] Decision caching per session still works.
- **Verify:** `go test ./internal/approval/... && go test ./internal/safety/... && go build ./...`

### T406 · Written-files registry
- **status:** todo · **priority:** P3 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** —
- **Why:** Knowing exactly what a turn wrote enables post-turn verification and safer merge/conflict decisions.
- **Scope:** new `internal/loop/written_files.go`; wire from mutation tools' results; tests.
- **Approach:** Track repo-relative paths written/deleted this session with a first-seen order. Expose a read accessor. Do not persist beyond the session. Feed existing `edit_verify.go` so verification covers exactly the touched set.
- **AC:**
  - [ ] Every mutating tool call records its target path(s), repo-relative.
  - [ ] Deletions are distinguishable from writes.
  - [ ] Registry is session-scoped with no cross-session bleed.
  - [ ] `edit_verify` consumes the registry without behavior regression.
  - [ ] Accessor is concurrency-safe (guarded) since tool dispatch may be concurrent for reads.
- **Verify:** `go test ./internal/loop/... -run TestWrittenFiles && go build ./...`

### T407 · Write-cap escape hatch + configurable cap
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** — (base cap landed ahead of the board in `0d0199b`)
- **Why:** The 8KB whole-file-rewrite cap (`internal/tools/write/write.go`) steers the model to `edit` (targeted changes only) or `git checkout` (*reverts* — it does not write new content). That leaves **no sanctioned path for legitimately replacing a large existing file wholesale**, which is a plausible new wedge: the model either flails against the cap or discovers the loophole (`rm` the file, then `write` — uncapped because the file no longer "exists"). Separately, the cap is a hard `const`, yet the tiny/local profile is precisely where output budgets are tightest and the cap arguably should be lower.
- **Scope:** `internal/tools/write/write.go`; `internal/config/types.go`, `internal/config/defaults.go` (+ profile defaults); tests in `internal/tools/write/write_test.go`.
- **Approach:** (a) Give the refusal a sanctioned route — either name delete-then-write explicitly in the error text, or add an explicit override argument the model can set when it genuinely means to replace the file. (b) Make the cap configurable (global default 8KB, overridable per profile), wiring the tiny profile to a tighter value. Keep the default behavior unchanged when unconfigured.
- **AC:**
  - [ ] A sanctioned path exists for wholesale replacement of a large existing file, and the error text names it.
  - [ ] Cap is configurable; unconfigured behavior is byte-identical to today (regression test).
  - [ ] Tiny profile may set a lower cap; the value is documented in the profile table.
  - [ ] The loophole (delete → write) is either documented as the sanctioned route or explicitly closed.
  - [ ] `write` remains model-facing only — no internal caller is affected.
- **Verify:** `go test ./internal/tools/write/... && go test ./internal/config/... && go build ./...`

---

## E5 — Extensibility

### T501 · Hook config schema
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** T502
- **Why:** Any extension surface starts with a config contract. Get the schema right and the runner is straightforward.
- **Scope:** `internal/config/types.go`, `internal/config/defaults.go`, config load tests.
- **Approach:** `[[hooks]]` entries with `event`, optional `matcher` (tool-name pattern), `command`, `args`, `timeout_ms`, and a per-hook context limit. Events: `session_start`, `session_end`, `user_prompt_submit`, `pre_tool_use`, `post_tool_use`, `subagent_start`, `subagent_stop`, `stop`, `pre_compact`, `post_compact`. Defaults disabled (no hooks configured ⇒ zero behavior change).
- **AC:**
  - [ ] Schema parses from `~/.bee/config.toml` with documented keys.
  - [ ] Unknown event names are rejected at load with a clear error.
  - [ ] Missing hooks ⇒ empty slice, no behavior change (regression test).
  - [ ] Timeout has a default and a sane clamp range.
  - [ ] Config round-trips (write→read) without loss.
- **Verify:** `go test ./internal/config/... && go build ./...`

### T502 · Bounded hook runner
- **status:** todo · **priority:** P2 · **size:** M · **owner:** builder
- **depends:** T501 · **blocks:** T503, T504, T206
- **Why:** External commands running inside the agent loop are a footgun unless every resource is bounded. Bound first, wire later.
- **Scope:** new `internal/hooks/` (`runner.go`, `contracts.go`); tests.
- **Approach:** Spawn a handler with a hard timeout and byte-capped stdout/stderr (cap ~64 KiB). Pass event context as JSON on stdin. Parse a bounded decision (allow/deny/context-injection). A handler that times out, exits non-zero, or floods output is logged and treated as **no decision** (fail-open for context, never a crash). Concurrency-safe with in-flight child tracking and clean shutdown.
- **AC:**
  - [ ] Timeout kills the child and returns no decision.
  - [ ] Output beyond the cap is truncated safely (no OOM, no partial-JSON crash).
  - [ ] Non-zero exit is non-fatal.
  - [ ] Malformed JSON decision is ignored, not fatal.
  - [ ] Runner has no import of `internal/loop` (no cycle).
  - [ ] Tests use a local test binary/stdin shim — never the network.
- **Verify:** `go test ./internal/hooks/... && go build ./...`

### T503 · PreToolUse / PostToolUse wiring
- **status:** todo · **priority:** P2 · **size:** M · **owner:** builder
- **depends:** T502
- **Why:** Tool-boundary hooks are the most useful extension point (policy gates, auto-format, audit trails).
- **Scope:** `internal/loop/turn_tools.go` (`dispatchTools`), `internal/hooks/` integration; tests.
- **Approach:** Before dispatch, run `pre_tool_use` (matcher-filtered). A deny stops the call and returns a tool error carrying the hook's reason. After dispatch, run `post_tool_use`; injected context is folded into the tool result. Respect the read-only-concurrent vs. mutator-serial barrier — hooks run inside the same scheduling discipline.
- **AC:**
  - [ ] A deny hook prevents execution and surfaces the reason to the model.
  - [ ] Injected post-hook context appears in the tool result the model sees.
  - [ ] Matchers filter correctly (only matching tool names fire).
  - [ ] No hooks configured ⇒ byte-identical behavior to today (regression test).
  - [ ] Hook failure never aborts the turn.
  - [ ] Concurrency: a hook on a read-only batch does not serialize the whole batch.
- **Verify:** `go test ./internal/loop/... -run TestHooks && go build ./...`

### T504 · Lifecycle events
- **status:** todo · **priority:** P3 · **size:** S · **owner:** builder
- **depends:** T502
- **Why:** Session/compaction/stop events let integrators observe and react without patching the loop.
- **Scope:** `cmd/bee/run.go`, `cmd/bee/tui.go`, `internal/loop/turn.go`, `internal/loop/compact.go`; tests.
- **Approach:** Emit `session_start`/`session_end`, `stop` at turn end, `pre_compact`/`post_compact` around compaction. Fire-and-forget: a slow or failing hook never delays the turn (bounded, best-effort).
- **AC:**
  - [ ] Each event fires at the documented point (assert via a recording test hook).
  - [ ] A failing hook does not affect turn outcome.
  - [ ] Session end fires even on error paths (once).
  - [ ] No hooks ⇒ no observable change.
- **Verify:** `go test ./internal/loop/... -run TestLifecycleHooks && go build ./...`

### T505 · Cron scheduler with active-hours + busy queue
- **status:** todo · **priority:** P2 · **size:** L · **owner:** builder
- **depends:** — · **blocks:** T506
- **Why:** `zzz` is a single overnight loop. Generalized scheduled work needs active-hours gating and a "never two at once" queue.
- **Scope:** new `internal/cron/` (`scheduler.go`, `active_hours.go`, `queue.go`, `registry.go`, `types.go`); tests.
- **Approach:** Config-defined jobs (`schedule`, `command`/skill, `active_hours`). Active-hours expresses a daily window; outside it, a job is deferred, not dropped. A busy-queue serializes runs per resource key so overlapping schedules cannot stack. No network, no daemon requirement for tests — the scheduler is driven by an injectable clock.
- **AC:**
  - [ ] A job outside active hours is deferred and runs when the window opens (fake clock).
  - [ ] Two jobs sharing a resource key never run concurrently.
  - [ ] A crashed job does not wedge the queue (released on completion or failure).
  - [ ] Scheduler is driven by an injectable clock; tests need no real time.
  - [ ] Empty config ⇒ scheduler idle, no goroutine leak.
- **Verify:** `go test ./internal/cron/... && go build ./...`

### T506 · `bee cron` subcommand + zzz integration
- **status:** todo · **priority:** P3 · **size:** M · **owner:** builder
- **depends:** T505
- **Why:** A scheduler needs a CLI and a way to coexist with the existing overnight driver.
- **Scope:** new `cmd/bee/cron.go`, `cmd/bee/main.go` (dispatch), `internal/zzz/`;
- **Approach:** `bee cron ls|run <id>|daemon`. `ls` prints configured jobs and next-run times. `daemon` runs the scheduler in the foreground. Ensure `zzz` and cron do not both drive the same worktree; document precedence.
- **AC:**
  - [ ] `bee cron ls` lists jobs deterministically.
  - [ ] `bee cron run <id>` executes a single job synchronously and exits non-zero on failure.
  - [ ] `bee cron daemon` runs until interrupted; Ctrl-C exits cleanly.
  - [ ] `zzz` and cron running together do not collide on one worktree (guard + test).
  - [ ] `bee help` lists the new subcommand.
- **Verify:** `go build ./... && ./bee cron ls && BEE_TEST_PROVIDER=stub ./bee cron run <test-job>`

---

## E6 — Goal & unattended loops

### T601 · Reply fingerprint (no-progress)
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** T602
- **Why:** An unattended loop that re-emits the same reply is not progressing. Hash the normalized reply and detect it.
- **Scope:** new `internal/goal/fingerprint.go`; tests.
- **Approach:** Normalize line endings and trim outer whitespace only; hash the rest verbatim so inner content stays semantic. Return empty for empty replies. Track the last fingerprint per compacted-goal thread.
- **AC:**
  - [ ] Line-ending and outer-whitespace variance produces the same fingerprint.
  - [ ] Any inner-text change changes the fingerprint.
  - [ ] Empty/whitespace-only replies return empty (not a hash).
  - [ ] Deterministic across runs.
- **Verify:** `go test ./internal/goal/... -run TestReplyFingerprint && go build ./...`

### T602 · Cost-aware verifier policy
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** T601 · **blocks:** T603
- **Why:** Silently doubling a user's bill to verify the user's own paid model is unacceptable. Verify only when it is our cost; otherwise accept the worker's proposal.
- **Scope:** new `internal/goal/verifier_policy.go`; tests.
- **Approach:** Map the active route to a verification mode: first-party-billed routes ⇒ `subagent` (read-only verifier); BYOK / user-key routes ⇒ `none` (accept proposal). Not a persisted user setting; recomputed per settlement. Config override allowed but off by default.
- **AC:**
  - [ ] Route mapping covers all existing routes and defaults to `none` for unknown.
  - [ ] Override config is honored when set.
  - [ ] Policy is a pure function with a table test.
  - [ ] No network call in the policy itself.
- **Verify:** `go test ./internal/goal/... -run TestVerifierPolicy && go build ./...`

### T603 · Read-only verifier subagent
- **status:** todo · **priority:** P2 · **size:** L · **owner:** builder
- **depends:** T602 · **blocks:** T604
- **Why:** Independent verification needs an isolated, read-only perspective — not the worker re-grading itself.
- **Scope:** new `internal/goal/verifier.go`; reuse `internal/loop/role.go` read-only tool whitelist; tests.
- **Approach:** Spawn a read-only engine with the read-only tool surface and a bounded evidence brief (objective + requirement list + observed artifacts). It returns a verdict with cited evidence, never edits. Budget-bounded; on failure, fall back to `none`.
- **AC:**
  - [ ] Verifier cannot call any mutating tool (whitelist enforced and tested).
  - [ ] Verdict includes evidence references, not just pass/fail.
  - [ ] Verifier failure degrades to accepting the worker proposal, never hangs.
  - [ ] Verifier run is budget-capped.
  - [ ] No provider name-drops in comments.
- **Verify:** `go test ./internal/goal/... -run TestVerifier && go build ./...`

### T604 · Replay-safe budget-limit turn
- **status:** todo · **priority:** P3 · **size:** M · **owner:** builder
- **depends:** T603
- **Why:** Hitting a token budget should produce a durable, summarizable turn that recovery can replay without re-spending.
- **Scope:** new `internal/goal/budget_turn.go`; `internal/loop/` integration; tests.
- **Approach:** Render a budget-limit prompt from a template with objective/time/tokens, wrapped in an internal-context fragment so it is clearly not user input. Derive a durable request id from goal id + decision epoch so startup recovery replays the same summary instead of starting a second model turn.
- **AC:**
  - [ ] Prompt renders the objective and numeric budget; template placeholders are XML-escaped.
  - [ ] Request id is stable for the same goal+epoch and differs for a new epoch.
  - [ ] Recovery replays without a duplicate model call (test with a stub provider counting calls).
  - [ ] The fragment is marked internal, not user-authored.
- **Verify:** `go test ./internal/goal/... -run TestBudgetTurn && go build ./...`

### T605 · Wire into `zzz` / `/goal`
- **status:** todo · **priority:** P2 · **size:** M · **owner:** builder
- **depends:** T604
- **Why:** Unwired machinery is dead code. This makes E6 real in the unattended paths.
- **Scope:** `internal/zzz/loop.go`, `cmd/bee/zzz.go`, `internal/goal/`, `cmd/bee/` goal command; tests.
- **Approach:** Use fingerprint + verifier policy + budget turn in the zzz loop and the `/goal` completion loop. Keep sentinel semantics: completion is only claimed when the verifier (or accepted proposal) plus fingerprint agree.
- **AC:**
  - [ ] `zzz` stops on verified completion and never on a repeated fingerprint alone.
  - [ ] `/goal` uses the same completion decision path.
  - [ ] A no-progress loop is detected and stopped with a typed reason.
  - [ ] Existing sentinel tests still pass.
  - [ ] Offline e2e: `BEE_TEST_PROVIDER=scripted` fixture drives a short zzz run to completion.
- **Verify:** `go test ./internal/zzz/... ./internal/goal/... && go build ./...`

---

## E7 — TUI & UX

### T701 · Status-line item registry + config
- **status:** todo · **priority:** P2 · **size:** M · **owner:** builder
- **depends:** — · **blocks:** T702, T703
- **Why:** The status bar is hardcoded in `view_chrome.go`. A registry + ordered config makes it user-definable and machine-drivable.
- **Scope:** new `internal/tui/statusline.go`; `internal/config/types.go` (`tui.status_line` string array); `internal/tui/view_chrome.go`; tests.
- **Approach:** Item registry mapping id → renderer. Config is an ordered array of ids; unknown ids ignored; duplicates: first wins; empty array hides the line; missing key ⇒ defaults. Missing/damaged config falls back silently and never blocks startup.
- **AC:**
  - [ ] Ordered config renders in that exact order.
  - [ ] Unknown id is ignored without breaking the rest.
  - [ ] Empty array hides the line; missing key uses defaults.
  - [ ] Damaged config does not block TUI startup.
  - [ ] At least the existing items are expressible: cwd, session title, git branch, model, context remaining.
  - [ ] Rendering is ANSI-width-safe (uses existing `lipglossWidth`/`truncateVisible`).
- **Verify:** `go test ./internal/tui/... -run TestStatusLine && go build ./...`

### T702 · Machine-readable status line
- **status:** todo · **priority:** P2 · **size:** S · **owner:** builder
- **depends:** T701
- **Why:** Scripts and `remote-control` need a strict record, not free text.
- **Scope:** `internal/tui/statusline_machine.go`, `internal/remote/`; tests.
- **Approach:** An exclusive mode that owns the whole line and emits one strict machine-readable record. Enabled only by explicit config (never a default). When active, no free-text item renders — this is what keeps the record parseable.
- **AC:**
  - [ ] Exclusive mode suppresses all other items.
  - [ ] Record is parseable by a single documented grammar (test parses it).
  - [ ] Not enabled by default (assert default config).
  - [ ] `remote-control` can consume the record.
- **Verify:** `go test ./internal/tui/... -run TestStatusMachine && go build ./...`

### T703 · Narrow-terminal fitting + aliases
- **status:** todo · **priority:** P3 · **size:** S · **owner:** builder
- **depends:** T701
- **Why:** A configurable line must degrade gracefully in a narrow terminal instead of wrapping/overflowing.
- **Scope:** `internal/tui/statusline_fit.go`; tests.
- **Approach:** Priority-ordered dropping as width shrinks; alias map so short/legacy names resolve to canonical ids; fitting uses existing width helpers so ANSI styling never miscounts.
- **AC:**
  - [ ] At 40/80/120 cols the line never exceeds width.
  - [ ] Dropping order is deterministic and prioritizes permission/context warnings.
  - [ ] Aliases resolve; canonical id listed renders the same.
  - [ ] ANSI-coloured items are measured correctly.
- **Verify:** `go test ./internal/tui/... -run TestStatusFit && go build ./...`

### T704 · Skills directory watcher (FS identity)
- **status:** todo · **priority:** P3 · **size:** M · **owner:** builder
- **depends:** — · **blocks:** —
- **Why:** Editing `~/.bee/skills/*.md` mid-session should take effect without a restart.
- **Scope:** new `internal/skills/watch.go`; `internal/skills/registry.go`; tests.
- **Approach:** Watch the skills dir; on change, reload the registry. Key the watcher by `(path, dev, ino)` so replacing a directory at the same path never reuses a stale watcher. Debounce rapid writes. Watcher teardown must not block on macOS.
- **AC:**
  - [ ] Adding/editing a skill file updates the registry without restart.
  - [ ] Replacing the directory at the same path attaches a fresh watcher.
  - [ ] Rapid successive writes coalesce (no reload storm).
  - [ ] Watcher close is prompt and idempotent.
  - [ ] No watch started when the dir is absent; a later creation is picked up if feasible, else documented.
- **Verify:** `go test ./internal/skills/... && go build ./...`

---

## E8 — Verification & enablement

### T801 · Offline e2e fixtures per subsystem
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** — · **blocks:** —
- **Why:** Every new subsystem needs a deterministic offline path or CI cannot gate it.
- **Scope:** `internal/llm/mockprov/`, scripted fixture JSONL files, `cmd/bee/stub_provider.go`; tests.
- **Approach:** Add scripted fixtures that drive: a runaway-loop turn (guard fires), a compaction turn (skip + commit), a hook deny, a cron job run, a budget-limit turn. One fixture per behavior.
- **AC:**
  - [ ] Each E1–E6 subsystem has a fixture that exercises it end-to-end offline.
  - [ ] `BEE_TEST_PROVIDER=scripted BEE_TEST_SCRIPT=<f> ./bee run --headless "..."` exits 0 per fixture.
  - [ ] No fixture requires network.
  - [ ] Fixtures are committed and referenced from the relevant package tests.
- **Verify:** `BEE_TEST_PROVIDER=stub ./bee run --headless "anything" && go test ./...`

### T802 · AGENTS.md + docs updates
- **status:** todo · **priority:** P1 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** —
- **Why:** bee's AGENTS.md is the contract agents read first. New surface must be documented or it will be misused.
- **Scope:** `AGENTS.md`, `docs/remaster/*`; per-ticket.
- **Approach:** Update the architecture section, env-var table, tools table, and conventions for each landed subsystem (guard config, hooks config, cron subcommand, status-line config, new tools).
- **AC:**
  - [ ] Every new config key and subcommand is documented.
  - [ ] New tools appear in the tools table.
  - [ ] No stale claims about removed heuristics.
  - [ ] Docs are English and match actual behavior (verified by running the command).
- **Verify:** review + `go build ./...`

### T803 · Bench regression gate
- **status:** todo · **priority:** P1 · **size:** M · **owner:** builder
- **depends:** — · **blocks:** —
- **Why:** Changes to prompts, reminders, and compaction are exactly the ones that silently regress the tiny profile bee is built for.
- **Scope:** `internal/bench/`, CI workflow; `docs/remaster/KANBAN.md` metrics.
- **Approach:** Record the M0 baseline (score + wall-clock). Add a gate that fails when the tiny-profile bench score drops below baseline minus a small tolerance. Gate runs on prompt/loop/convention changes.
- **AC:**
  - [ ] Baseline recorded in the board before the first E2/E3 ticket lands.
  - [ ] Gate fails on a deliberate regression (verified once locally).
  - [ ] Gate tolerance is documented and not so loose it is meaningless.
  - [ ] Gate runs offline against the stub/scripted provider.
- **Verify:** `go test ./internal/bench/... && (breach test) && go test ./...`

### T804 · File-size / split hygiene pass
- **status:** todo · **priority:** P3 · **size:** S · **owner:** builder
- **depends:** — · **blocks:** —
- **Why:** The ≤300-line convention is easy to violate mid-remaster and hard to unwind later.
- **Scope:** touched files only; enforced as part of review.
- **Approach:** Before opening a PR, split any file nearing 300 lines along behavior seams (mirror the existing `wire/openai_stream.go` split style).
- **AC:**
  - [ ] No file introduced by the remaster exceeds 300 lines.
  - [ ] Splits preserve package boundaries and test coverage.
  - [ ] `go build ./...` and `go test ./...` stay green after each split.
- **Verify:** `go build ./... && go test ./...` and a line-count check in review.

---

## Change log

| Date | Change | By |
|---|---|---|
| 2026-09-20 | Initial board, 41 tickets across 8 epics | PM |
| 2026-09-20 | Add T407 (write-cap escape hatch + configurable cap); base cap landed ahead of the board in `0d0199b`. Board now 42 tickets. | PM |