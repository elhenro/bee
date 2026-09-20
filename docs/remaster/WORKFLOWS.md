# bee Remaster — Agent Workflows

This is the operating manual for agents working the [KANBAN](./KANBAN.md). Read it once in full before pulling a card. It defines roles, the ticket lifecycle, the gates every change must pass, and the rules that let multiple agents work the same repo without stepping on each other.

---

## 1. Roles

| Role | Session kind | May write files? | Responsibility |
|---|---|---|---|
| **PM** | orchestrator (root) | Board + docs only | Owns the board, definitions, sequencing, unblocking. Does not implement tickets. |
| **Builder** | worker | Yes (own branch) | Implements exactly one ticket. Owns its branch from pull to review. |
| **Verifier** | verifier | No | Independently validates a card in Review. Reproduces evidence, does not fix it. |
| **Integrator** | orchestrator/worker | Yes (merge commit) | Merges reviewed branches, resolves conflicts, runs full gates on the merge. |

Rules:
- One builder owns one card at a time. Do not start a second card in the same session.
- A verifier never edits project files. Findings go in the card's worklog.
- The PM never marks a card Done; only green gates on the merge commit do.

---

## 2. Non-negotiable rules (every ticket)

1. **Pure Go, no CGo.** Single static binary must stay buildable on darwin/linux/windows.
2. **Files ≤300 lines.** Split before exceeding; follow the `wire/openai_stream.go` split style.
3. **No provider name-drops in code comments.** Describe behavior, not vendor. Vendor names are fine in config defaults and user-facing strings.
4. **Tests are first-class.** Every behavioral change ships `_test.go` coverage in the same commit.
5. **Never hit a real API from a test.** Use `BEE_TEST_PROVIDER=stub` or `scripted` with a fixture.
6. **Internal types own the wire boundary.** No provider SDK types leak upward.
7. **Don't expand scope.** If you find adjacent work, file a new card idea in the card worklog; do not implement it here.
8. **English** for code, comments, commits, and docs.

---

## 3. Definition of Ready (backlog → ready)

A card may move to Ready only when all hold:
- [ ] Spec exists in [TICKETS.md](./TICKETS.md) with non-empty Goal, Scope, AC, Verify.
- [ ] Every AC is objectively checkable (a command or an assertion, not a vibe).
- [ ] Scope names concrete files/dirs.
- [ ] All `depends` tickets are Done (or the dependency is explicitly waived by the PM with a reason).
- [ ] No other Ready/In-Progress card owns the same files (see §7 conflict map).

---

## 4. Definition of Done (review → done)

A card is Done only when all hold **on the merge commit**:
- [ ] Every acceptance criterion in the ticket is checked, with evidence in the worklog.
- [ ] Gates **G0–G4** green (§5). G5 green when the card touches prompts/loop/profile.
- [ ] New tests added and passing; no existing test weakened or deleted to pass.
- [ ] No file >300 lines; no new CGo; no new real-API dependency in tests.
- [ ] Docs updated if the public surface changed (T802 owns this; builders flag it).
- [ ] At most one reviewer-mandated change round remaining, or none.

---

## 5. Gates

Run in order. Report the exact commands and their results in the worklog — never claim a gate you did not run.

| Gate | Command | When |
|---|---|---|
| **G0 Build** | `go build ./...` | Always, before every commit |
| **G1 Focused test** | `go test ./internal/<pkg>/... -run <TestName>` | While iterating |
| **G2 Vet + lint** | `go vet ./...` && `golangci-lint run` | Before Review |
| **G3 Full suite** | `go test ./...` | Before Review and on merge |
| **G4 Offline e2e** | `BEE_TEST_PROVIDER=stub ./bee run --headless "anything"` (+ scripted fixture when the card adds one) | Before Review |
| **G5 Bench** | `bee bench` (tiny profile) | Cards touching prompts, reminders, compaction, profile defaults, or loop control |

Merge gate: the Integrator re-runs **G0, G3, G4** on the merge commit. A branch that was green in isolation is not trusted to be green after merge.

---

## 6. Ticket lifecycle

```
📥 Backlog ──(DoR met)──▶ 🔜 Ready ──(builder pulls)──▶ 🚧 In Progress
   🚧 ──(G0–G2 + evidence)──▶ 👀 Review ──(verifier validates)──▶ ✅ Done
   any ──(dependency/blocker)──▶ ⛔ Blocked ──(resolved)──▶ 🔜 Ready
   👀 ──(verifier finds defect)──▶ 🚧 In Progress (same builder, one round)
```

### Pulling a card (Builder)
1. Claim it: move the card to 🚧 In Progress and add a worklog line with the branch name and session id.
2. Branch: `remaster/<ticket-id>-<slug>` (e.g. `remaster/t101-abab-cycle`). Never work on the default branch.
3. Read the ticket spec and the files in Scope before writing anything.
4. Implement + test. Run G0/G1 continuously.
5. Run G1/G2/G3/G4 (+G5 if applicable). Move to 👀 Review and post the evidence block.
6. Assign a verifier (a fresh session with no context from yours).

### Worklog format (append to the ticket)
```
### Worklog
- 2026-09-20 · builder · branch remaster/t101-abab-cycle
  - G0: pass. G1: `go test ./internal/loop/... -run TestABAB` pass (7 subtests).
  - AC: 5/5 checked. Evidence: cycle_detect_test.go:TestABAB*
  - Notes: buffer length fixed at 4 per spec.
```

### Verifying a card (Verifier)
- Re-run every command in the ticket's Verify line on the branch, from a clean checkout.
- Check each AC independently; do not read the builder's worklog before forming your own conclusion.
- Bypass hunting: try to falsify the strongest claim (e.g. for a guard, construct an input that should NOT fire and confirm it doesn't).
- Report: `VERIFIED` or `DEFECT(<AC id>, <repro>)`. No edits. On defect, return the card to In Progress with a one-round budget.

---

## 7. Parallelization & the conflict map

Multiple builders may run at once **only** if their file sets are disjoint. The conflict map below defines ownership clusters. **Cards in the same cluster serialize.**

| Cluster | Files | Cards |
|---|---|---|
| C1 loop-repeat | `internal/loop/repeat_detect.go`, `turn_repeat.go`, `run_state.go` | T101, T102, T103, T104 |
| C2 loop-guard | `internal/loop/guard/*` | T105, T106 |
| C3 loop-context | `internal/loop/context.go`, `turn_compact.go`, `context_warning.go` | T203 |
| C4 loop-compact | `internal/loop/compact.go`, `cutpoint.go`, `compact_notice.go` | T204, T205, T206 |
| C5 loop-dispatch | `internal/loop/turn_tools.go` | T503, T406(reg.) |
| C6 prompt | `internal/prompt/assemble.go`, `reminders/*` | T301, T302, T304 |
| C7 config | `internal/config/types.go`, `defaults.go` | T501, T701, T202(knob) |
| C8 safety-token | `internal/safety/shell_tokenize.go`, `bash_ast.go`, `bash_split.go` | T401, T402 |
| C9 safety-policy | `internal/safety/safe_command.go`, `slow_command.go`, `patterns.go` | T403, T404 |
| C10 approval | `internal/approval/*` | T405 |
| C11 hooks | `internal/hooks/*` | T502, T503, T504, T206 |
| C12 cron | `internal/cron/*`, `cmd/bee/cron.go`, `cmd/bee/main.go` | T505, T506 |
| C13 goal | `internal/goal/*` | T601, T602, T603, T604, T605 |
| C14 tui-status | `internal/tui/statusline*.go`, `view_chrome.go` | T701, T702, T703 |
| C15 skills | `internal/skills/*` | T704 |
| C16 llm-budget | `internal/llm/budget.go`, `internal/loop/turn_run.go` | T202 |
| C17 bench/ci | `internal/bench/*`, CI workflow | T803 |

### Safe parallel waves (suggested)
- **Wave A:** T101 · T401 · T701 · T601 · T505 (all distinct clusters)
- **Wave B (after A):** T102 · T402 · T702 · T602 · T501
- **Wave C:** T104 · T403/T404 · T603 · T502 · T802
- **Wave D:** T103/T105 · T405 · T503 · T801 · T803

> Two cards that only *read* a shared file (e.g. both call `estimateMessageTokens`) do not conflict. Only writers serialize. The PM decides ties.

### Worktree discipline
Each builder works in its own git worktree (`internal/worktree` supports this). Never share a working tree between concurrent builders — bee's own `bee agents` model exists for exactly this reason.

---

## 8. Blocker protocol (three-turn rule)

Do not mark a card Blocked on first contact with an obstacle.
1. **Turn 1:** attempt a concrete alternative. Record what you tried.
2. **Turn 2:** inspect current state/evidence again; try a different controlled variable. Still stuck? Escalate to the PM in the worklog.
3. **Turn 3:** if the same condition still blocks progress, move the card to ⛔ Blocked with: the exact blocker, the ticket/dependency that must land first, and what you would do once unblocked.

Immediate Blocked (no three-turn wait), by analogy to the safety-refusal exception:
- A hard safety/policy boundary makes the ticket un-pursuable as written.
- A required dependency ticket was contradicted or reverted.

> Do not use Blocked for questions the PM or a dependency can answer — that is a normal worklog comment.

---

## 9. Merge protocol (Integrator)

1. Confirm the branch is based on current `main`; rebase if not.
2. Resolve conflicts **in favour of the ticket's intent**, preserving unrelated changes.
3. Re-run **G0, G3, G4** on the merge commit. G5 if flagged.
4. Merge; move the card to ✅ Done with the merge commit hash in the worklog.
5. If the merge breaks a previously-Done card's tests, reopen that card rather than patching over it.

Branch naming: `remaster/<ticket-id>-<slug>`. Commits: Conventional Commits (`feat(loop):`, `fix(safety):`, `test(...)`, `docs(...)`).

---

## 10. Explicit non-goals

Out of scope for this remaster. Filing a card for these is a PM decision, not a builder's:
- Hosted-service scaffolding: OAuth/token lease brokers, cloud permission classifiers, telemetry, managed connectors, plugin marketplaces.
- Monorepo release-inventory machinery or any file-inventory manifest.
- Any CGo dependency or anything that breaks the single static binary.
- Any required network call in the test suite.
- A rewrite of the provider/wire layer — the remaster builds **on** the existing spine, not around it.

---

## 11. Where the ideas came from

Each ticket cites the behavior it ports. Design reference (read-only, do not vendor or copy code):
- `packages/agent-modules/runaway-guard/*` — E1 (signals, priority order, reserve-before-emit, non-durable reminders, verified-progress exemption).
- `packages/agent-modules/context-manager/*` — E2 (token estimator, provider budget, trigger math, typed skip reasons).
- `packages/agent-modules/system-reminder/*` — E3 (provider registry with critical/droppable tiers, todo-state suppression).
- `packages/agent-modules/permission/*` — E4 (checker registry, decision reducer, bash AST, safe-command whitelist, slow-command scanner, written-files registry).
- `packages/agent-modules/plugin-hooks/*` — E5 (event list, bounded runner).
- `packages/agent-modules/cron/*` — E5 (active hours, busy queue).
- `packages/agent-modules/goal/*` — E6 (reply fingerprint, cost-aware verifier policy, replay-safe budget turn).
- `packages/tui/docs/status-line-config.md` — E7 (ordered item config, machine-readable mode, fitting rules).
- `packages/agent-modules/skills/directory-watcher.ts` — E7 (FS-identity watcher keying).

Port behavior and invariants. Never copy TypeScript structure into Go, and never vendor the repository.

---

## 12. Kickoff checklist (PM)

- [ ] Record M0 baseline: `bee bench` score, `go test ./...` wall-clock, current `go build ./...` cleanliness.
- [ ] Confirm G0–G4 all pass on `main` before the first pull.
- [ ] Move Wave A cards to 🔜 Ready.
- [ ] Create the `remaster/*` branch convention note in the repo (done via this file).
- [ ] Assign one builder session per Wave A card, each in its own worktree.