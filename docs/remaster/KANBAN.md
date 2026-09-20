# bee Remaster — Kanban Board

**Program:** Remaster bee toward the loop-resilience / context-economics / extensibility direction distilled from `MiniMax-AI/minimax-code`.
**Board owner (PM):** Mavis (orchestrator).
**Executors:** Builder / Verifier / Integrator agents (see [WORKFLOWS.md](./WORKFLOWS.md)).
**Status of this document:** live board. Move cards between columns; do not rewrite history — append to ticket worklogs.

Companion files:

| File | Purpose |
|---|---|
| [KANBAN.md](./KANBAN.md) | This board. Column state, WIP limits, epics, sequencing. |
| [TICKETS.md](./TICKETS.md) | Full spec for every ticket (goal, scope, AC, verification). |
| [WORKFLOWS.md](./WORKFLOWS.md) | Agent roles, ticket lifecycle, gates, merge protocol, conflict map. |

---

## 1. The bet

bee already has a solid spine (types → provider → tools → loop → ui) and several of the target mechanisms in embryo: `repeat_detect.go`, `escalate`, `compact.go`, `done_signal.go`, `sentinel`, `sandbox`, `approval`. The remaster is **not a rewrite**. It is:

1. **Deepening the loop** — replace exact-repeat detection with a real turn-local runaway guard (ABAB cycles, error families, polling streaks, verified-progress exemptions).
2. **Making context economics first-class** — dynamic output budgets, proactive compaction triggers, safe cut-points, typed skip reasons.
3. **Consolidating prompt reminders** — a provider registry with critical/droppable tiers instead of ad-hoc injection.
4. **Hardening safety** — a shell tokenizer + word-only parser + read-only whitelist + slow-command perf guard behind a permission checker registry.
5. **Opening an extension surface** — bounded lifecycle hooks and a cron scheduler, config-driven.
6. **Upgrading unattended loops** — reply fingerprints, cost-aware verification, replay-safe budget turns.
7. **Finishing the UX** — configurable + machine-readable status line, hot-reloaded skills.

Non-goals (explicitly out of scope, see WORKFLOWS §10): hosted-service scaffolding (OAuth lease brokers, cloud permission classifier, telemetry, managed connectors), monorepo release-inventory machinery, any CGo dependency, any new required network call in tests.

### Landed ahead of the board

Work already merged to the remaster branch before ticketing, in the E1/E4 direction:

| Commit | Change | Board overlap |
|---|---|---|
| `f345bde` | `StripBrokenToolCall` — drop the wedging text preamble on provider-400 resume | E1 (wedge recovery) |
| `0d0199b` | Refuse whole-file rewrites of existing files above 8KB | E4 seeds **T407** |
| `7062718` | Render mid-compact queued submit inline instead of a scrollback echo | E7 (UX) |

Branch: `fix/provider-400-wedge-hardening`. T407 exists specifically to close the gap these commits left open (no sanctioned wholesale-replacement path; cap not configurable).

---

## 2. Board

WIP limits are hard. A column over its limit means **stop pulling**, not "just this once".
`🚧` limit 3 · `👀` limit 2 · `⛔` no limit (blocked cards must have a one-line reason).

| 📥 Backlog | 🔜 Ready | 🚧 In Progress (WIP 3) | 👀 Review (WIP 2) | ✅ Done |
|---|---|---|---|---|
| T104 T203 T205 T206 T303 T304 T402 T404 T406 T407 T501 T503 T504 T506 T603 T604 T605 T703 T704 T802 T804 | T103 T105 T106 T204 T302 T403 T405 T502 T801 | T803 (`582fd4f`) T202 T602 | T301 (`35e9c57`) T702 (`9e25897`) | T101 T401 T701 T102 T601 T505 T201 |

⛔ **Blocked:** _(none yet)_

> **Done (7):** T101 `379d442` · T401 `db4a68d` · T701 `aa663ec` · T102 `1037bbc` · T601 `1037bbc`
> · T505 `f97791a` · T201 `f97791a`. All independently VERIFIED; merge gates re-run on the
> integration tip (last: G0 ✅ · G2 ✅ 0 issues · G3 ✅ 50.2s · G4 ✅ · G5 ✅ 34.3 == M0).
> **In flight:** T202 (C16) · T602 (C13) · T803 built `582fd4f`, awaiting a Review slot.
> **Review:** T301 · T702.
>
> ⚠ **T505 follow-up (for T506):** the verifier found that the **first run ignores the cron
> `schedule`** — `New` seeds `nextDue = clock.Now()`, so every job fires on the first tick after
> process start regardless of its expression (then follows the schedule). This is outside T505's
> ACs and was documented by the builder as intended "due on first tick", but a yearly job firing on
> every restart is likely wrong. **T506 must resolve it** (seed `nextDue = Schedule.Next(now)`) or
> explicitly bless run-on-start in docs. Not blocking T505 (ACs green); filed here so it is not lost.

### Column rules (quick)
- **📥 Backlog** — spec exists in TICKETS.md, dependencies may be unmet.
- **🔜 Ready** — passes Definition of Ready (WORKFLOWS §3): scope files named, AC testable, verification command present, no unmet dependency.
- **🚧 In Progress** — exactly one card per builder agent; branch pushed; worklog opened.
- **👀 Review** — builder has run G0–G2 locally and attached evidence; a Verifier is assigned.
- **✅ Done** — all gates G0–G4 green on the merge commit, ACs checked, docs updated.
- **⛔ Blocked** — record the blocker + the ticket that must land first.

---

## 3. Epics

| Epic | Name | Outcome | Tickets |
|---|---|---|---|
| **E1** | Loop resilience | Turn-local runaway guard with typed signals and non-durable reminders | T101–T106 |
| **E2** | Context economics | Dynamic output budget, proactive triggers, safe compaction, typed skip reasons | T201–T206 |
| **E3** | Prompt & reminder architecture | Reminder provider registry + plan tracking | T301–T304 |
| **E4** | Safety & permission | Shell-parse-grounded permission checker registry + perf guard | T401–T407 |
| **E5** | Extensibility | Bounded lifecycle hooks + cron scheduler | T501–T506 |
| **E6** | Goal & unattended loops | Fingerprints, cost-aware verification, replay-safe budget turns | T601–T605 |
| **E7** | TUI & UX | Configurable/machine status line, hot-reload skills | T701–T704 |
| **E8** | Verification & enablement | Fixtures, docs, bench gate, file-size hygiene | T801–T804 |

---

## 4. Sequencing / dependency DAG

```
E1:  T101 ─┐
     T102 ─┼─▶ T103 ─▶ T105 ─▶ T106
     T104 ─┘
E2:  T201 ─▶ T202 ─▶ T203 ─▶ T204 ─▶ T205 ─▶ T206 (needs T502)
E3:  T301 ─▶ T302
     T303 ─▶ T304 (needs T301)
E4:  T401 ─▶ T402 ─▶ T403
                 └─▶ T404
     T405 (needs T402+T403) ─▶ T406
E5:  T501 ─▶ T502 ─▶ T503
                 └─▶ T504
     T505 ─▶ T506
E6:  T601 ─▶ T602 ─▶ T603 ─▶ T604 ─▶ T605
E7:  T701 ─▶ T702
     T703, T704 independent
E8:  T801, T802, T803, T804 continuous
```

### Milestones
| # | Name | Exit criteria | Epics |
|---|---|---|---|
| M1 | Foundation | E1 + E4 merged; guard signals observable; permission registry live behind a flag | E1, E4 |
| M2 | Context | E2 + E3 merged; tiny-profile bench non-regressed | E2, E3 |
| M3 | Extensibility | E5 + E6 merged; hooks + cron documented | E5, E6 |
| M4 | UX | E7 merged; status line config stable | E7 |
| M5 | Hardening | E8 complete; all gates green on `main` | E8 |

---

## 5. Critical path & pull order

Recommended first pulls (highest leverage, lowest coupling):

1. **T101** → **T102** → **T103** (E1 core signals; each own file so they parallelize after T101 lands the ring-buffer helper).
2. **T401** → **T402** → **T403** (E4 foundation; T403 removes the most friction from auto-approve).
3. **T201** → **T202** (E2 dynamic budget; biggest tiny/local-profile win).

Critical path to M1: `T101 → T105 → T106` and `T401 → T402 → T405`.

---

## 6. Card detail

Every card's full spec lives in [TICKETS.md](./TICKETS.md). Card IDs are stable and must not be renumbered.

| ID | Title | Epic | Pri | Size | Depends |
|---|---|---|---|---|---|
| T101 | ABAB-cycle oscillation signal | E1 | P1 | S | — |
| T102 | Error-family clustering signal | E1 | P1 | S | — |
| T103 | Verified-progress exemption | E1 | P1 | M | T101, T102 |
| T104 | Unchanged-output polling streak guard | E1 | P2 | S | — |
| T105 | Turn-local guard observer + reminder policy | E1 | P1 | L | T103, T104 |
| T106 | Non-durable reminder hardening | E1 | P1 | S | T105 |
| T201 | Token estimator interface | E2 | P1 | S | — |
| T202 | Dynamic max-output resolver | E2 | P1 | M | T201 |
| T203 | Proactive compaction trigger | E2 | P1 | M | T202 |
| T204 | Safe cut-point selector | E2 | P1 | M | T203 |
| T205 | Typed compaction skip reasons + events | E2 | P2 | S | T204 |
| T206 | Compaction deferral hook | E2 | P2 | S | T205, T502 |
| T301 | Reminder provider registry | E3 | P1 | M | — |
| T302 | Migrate ad-hoc reminders to providers | E3 | P1 | M | T301 |
| T303 | Plan (todo) tool | E3 | P2 | M | — |
| T304 | Stale-plan reminder provider | E3 | P2 | S | T301, T303 |
| T401 | Quote-aware shell tokenizer | E4 | P1 | M | — |
| T402 | Word-only command-sequence parser + splitter | E4 | P1 | L | T401 |
| T403 | Read-only safe-command whitelist | E4 | P1 | M | T402 |
| T404 | Slow-command performance guard | E4 | P2 | M | T401 |
| T405 | Permission checker registry + decision reducer | E4 | P1 | L | T402, T403 |
| T406 | Written-files registry | E4 | P3 | S | — |
| T407 | Write-cap escape hatch + configurable cap | E4 | P2 | S | — |
| T501 | Hook config schema | E5 | P2 | S | — |
| T502 | Bounded hook runner | E5 | P2 | M | T501 |
| T503 | PreToolUse / PostToolUse wiring | E5 | P2 | M | T502 |
| T504 | Lifecycle events (start/stop/compact) | E5 | P3 | S | T502 |
| T505 | Cron scheduler with active-hours + queue | E5 | P2 | L | — |
| T506 | `bee cron` subcommand + zzz integration | E5 | P3 | M | T505 |
| T601 | Reply fingerprint (no-progress) | E6 | P2 | S | — |
| T602 | Cost-aware verifier policy | E6 | P2 | S | T601 |
| T603 | Read-only verifier subagent | E6 | P2 | L | T602 |
| T604 | Replay-safe budget-limit turn | E6 | P3 | M | T603 |
| T605 | Wire into `zzz` / `/goal` | E6 | P2 | M | T604 |
| T701 | Status-line item registry + config | E7 | P2 | M | — |
| T702 | Machine-readable status line | E7 | P2 | S | T701 |
| T703 | Narrow-terminal fitting + aliases | E7 | P3 | S | T701 |
| T704 | Skills directory watcher (FS identity) | E7 | P3 | M | — |
| T801 | Offline e2e fixtures per subsystem | E8 | P1 | M | — |
| T802 | AGENTS.md + docs updates | E8 | P1 | S | — |
| T803 | Bench regression gate | E8 | P1 | M | — |
| T804 | File-size / split hygiene pass | E8 | P3 | S | — |

---

## 7. Board metrics (update at each milestone)

| Metric | M0 (baseline) | M1 | M2 | M3 | M4 | M5 |
|---|---|---|---|---|---|---|
| Tickets Done | **7 / 42** | | | | | |
| Bench score (tiny profile) | **34.3** (holdout 50.4) | | | | | |
| `go test ./...` duration | **42.5s** (`-count=1`) | | | | | |
| Open blockers | 0 | | | | | |

> **M0 recorded 2026-09-20** on the merge base `main@8ee9812` (fast-forward of
> `fix/provider-400-wedge-hardening`; tree identical to that branch tip). Provider: `stub` for
> both bench and e2e (offline). Full gate sweep: G0 `go build ./...` ✅ · G2 `go vet ./...` ✅ +
> `golangci-lint run` ✅ 0 issues (lint 2.13.2/go1.27.1) · G3 `go test ./... -count=1` ✅ 42.48s ·
> G4 `BEE_TEST_PROVIDER=stub ./bee run --headless` ✅ exit 0 · G5 bench ✅ 34.3. Bench JSON:
> `bench/results/m0-baseline.json` (gitignored).
>
> **Integration trunk:** `remaster/integration` (branched from `main@8ee9812`). Builders branch
> `remaster/<ticket-id>-<slug>` off it; the Integrator merges reviewed branches here. `main` is
> advanced only on an explicit PM/user decision.