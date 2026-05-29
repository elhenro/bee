# bee bench + small-model tuning harness — design

Date: 2026-05-29
Status: approved (brainstorming), pending implementation plan

## Problem

bee is meant to scale down to tiny local models (4k-context Ollama, small MLX
fine-tunes). There is no objective, repeatable way to measure how well a given
model + bee config combination actually behaves on real coding tasks. Without a
score we cannot tell whether a prompt/profile/tool-surface change helps or hurts
small models — tuning is guesswork.

Goal: a measurement subcommand (`bee bench`) plus a Claude Code skill that drives
an iterative improve loop against a local omlx-served model
(`Qwen3.6-35B-A3B-UD-MLX-4bit`), so bee's small-model behavior can be tuned
empirically.

## Architecture — two pieces, clean split

```
bee bench   →  Go subcommand. measures, emits JSON scoreboard. deterministic, never mutates config.
tune skill  →  Claude Code skill, human/Claude driven. reads scoreboard, tweaks a knob, re-runs, keeps wins.
```

One measures, one decides. bench has no opinion about how to improve bee; the
skill has no scoring logic of its own. They communicate through the results JSON.

The agentic run per task reuses bee's existing `/goal` loop
(`internal/goal` + `runGoalHeadless`), which already returns a `Verdict{Met,Reason}`
and the full transcript.

## Component 1 — `bee bench` subcommand

New package `internal/bench` + `cmd/bee/bench.go`. Lives in the binary, fits the
"skills are bee subcommands" philosophy.

### Task suite

Directory of task specs, default `bench/tasks/*.json`. One task:

```json
{
  "id": "edit-add-flag",
  "prompt": "add a --verbose bool flag to cmd/foo/main.go, default false",
  "setup": "cp -r fixtures/foo $SANDBOX/",
  "checks": [
    {"kind": "cmd",  "run": "cd $SANDBOX && go build ./...", "expect_exit": 0},
    {"kind": "grep", "file": "$SANDBOX/cmd/foo/main.go", "pattern": "verbose"}
  ],
  "budget": {"max_turns": 12, "max_tokens": 40000},
  "judge": "the --verbose flag is wired and the code compiles"
}
```

- `prompt` — the goal condition handed to bee as `/goal <prompt>`.
- `setup` — optional shell, scaffolds an isolated sandbox tempdir (`$SANDBOX`).
- `checks` — objective truth. `kind: cmd` (run + expected exit) and
  `kind: grep` (file + pattern) for the skeleton. Cheap, deterministic.
- `judge` — LLM-judge fallback text, used when objective checks cannot capture
  success. Scored via the existing `goal.Evaluate`.
- `budget` — caps for the goal loop; also the denominator for efficiency scoring.

### Runner — subprocess, serial

Per task:

1. create fresh tempdir, export as `$SANDBOX`.
2. run `setup` (if any).
3. spawn the real binary: `bee run --headless "/goal <prompt>"` with
   `cmd.Dir = $SANDBOX`, env pointed at the omlx provider/model.
4. capture stdout/stderr + the session `.jsonl` written for that run.
5. run `checks` against the sandbox. If none, fall back to `judge` verdict
   (parsed from goal loop output / re-evaluated from transcript).

Subprocess (not in-process engine) because it exercises the real binary path
users hit, gives clean cwd isolation via `cmd.Dir` (no `os.Chdir` race), and bee
already writes a session jsonl with `tool_use` / `tool_result` blocks to mine for
metrics. Serial execution for determinism — small models are latency-bound, so
parallelism buys little and costs reproducibility.

### Scorer — 3 dimensions → 0-100

Session jsonl schema confirmed sufficient: `types.ContentBlock` carries
`Use *ToolUse{ID,Name,Input}` and `Result *ToolResult{UseID,Content,IsError}`;
message roles give turn counts.

| dim | source | measures |
|-----|--------|----------|
| **success** | `checks` pass + goal `Met` | did the task actually get done. dominant. |
| **efficiency** | turns / tool calls / tokens vs `budget` | small-model thrash even when it eventually passes |
| **format** | parse session jsonl | malformed tool-call JSON, unknown/hallucinated tool names, `IsError` tool results, clean stop vs cap-exceeded |

Per-task score = weighted sum, default weights `success 0.6 / format 0.25 /
efficiency 0.15`. Rationale: success dominates (a model that thrashes but never
works should score low regardless); format is the next most common small-model
failure; efficiency is a tiebreaker. Weights overridable via flag.

- success: `1.0` if all objective checks pass (or judge `Met` when no checks),
  else `0.0`. Binary — partial credit invites gaming.
- efficiency: `clamp(0,1, 1 - used/budget)` averaged across turns/tools/tokens
  actually present. Tokens best-effort (from session/cost data if recorded,
  dropped from the average otherwise).
- format: `1 - (malformed + unknown + errored_calls) / total_calls`, floored at 0;
  a run that hit the cap instead of stopping cleanly takes a fixed penalty.

Suite aggregate = mean of per-task scores, plus per-dimension means so the skill
can see *which* dimension is weak.

### Output

- `bench/results/<runlabel>.json` — machine-readable, one object: suite aggregate,
  per-dim means, per-task breakdown (score, dims, raw metrics, pass/fail, reason).
  `runlabel` defaults to a caller-supplied `--label` (the config-variant tag) so
  the skill can diff A vs B across files. No wall-clock timestamp baked into the
  scored content (keeps diffs clean; the file mtime carries time).
- stdout — human table: task | score | success | format | eff | notes.

Flags: `--suite <dir>`, `--label <tag>`, `--json`, `--weights s,f,e`,
`--task <id>` (run one), `--provider`/`--model` passthrough (else inherit config).

## Component 2 — tune skill (Claude Code)

A skill I (Claude) drive. Not built into bee.

Preconditions: omlx already serving `Qwen3.6-35B-A3B-UD-MLX-4bit` at
`localhost:8000`; bee config points at that provider/model. Skill assumes the
server is up (does not start/manage it) and fails fast with a clear message if not.

Loop:

1. run `bee bench --json --label baseline` → baseline scoreboard.
2. identify worst task × worst dimension from per-dim means.
3. hypothesize ONE minimal tweak — system-prompt wording, tiny-profile budget,
   a tool description, caveman level. One variable at a time.
4. apply the tweak, re-run `bee bench --json --label <tweak-tag>`.
5. diff aggregate + per-task vs the previous best. Keep if up, revert if down.
6. journal every iteration: what changed, score delta, kept/reverted. Append-only
   markdown under `bench/journal/`.
7. stop on user's word or when iterations stop yielding gains.

Guardrails:

- only ever tune bee knobs, never the task suite — tuning the test set overfits
  and invalidates the score.
- one change per iteration, always journaled and reversible.
- log any silent cap (e.g. "stopped after N no-gain rounds") to the journal.

## Scope — walking skeleton first

First build:

- `internal/bench` + `cmd/bee/bench.go`: runner (subprocess, serial), all three
  scorers, JSON + table output.
- 1–2 real tasks in `bench/tasks/` with fixtures (one edit task with a compile +
  grep check; optionally one read/answer task with a judge condition).
- the tune skill, proven end-to-end against live omlx for at least one
  tweak→re-run→compare cycle.

Deferred until the loop works end-to-end:

- broad task suite (10+ across edit/read/shell/search categories).
- token accounting if not already surfaced in the session record.
- parallel/sharded runs.
- variant-matrix mode (run N configs in one invocation).

## Testing

- bench scorer: unit tests on synthetic session jsonl fixtures (malformed call,
  unknown tool, errored result, clean run) asserting expected dim scores.
- runner: a scripted/mock-provider task (reuse `mockprov` / `mock_scenarios`)
  exercising the full run→check→score path without needing a live model, so CI
  stays deterministic and offline. Live-omlx runs stay manual / behind a flag.
- checks engine: unit tests for `cmd` and `grep` check kinds incl. failure paths.

## Open questions

None blocking. Token accounting fidelity (component of efficiency) to be settled
during implementation based on what the session/cost layer already records.
