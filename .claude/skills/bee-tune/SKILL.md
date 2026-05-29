---
name: bee-tune
description: Empirically tune bee's small-model behavior with `bee bench`. Use when the user wants to benchmark bee against a local model, improve small-model behavior, raise the bench score, or iterate on prompt/profile/tool tweaks. Drives a measure → tweak → re-measure loop against the bench suite.
---

# bee-tune — small-model improvement loop

`bee bench` measures. This skill is the loop on top: read the scoreboard, change
ONE knob, re-measure, keep wins, revert losses, journal everything.

The bench harness never mutates bee config. You do — one variable at a time.

## Preconditions

1. omlx (or another local server) is already serving the target model at its
   endpoint. This skill does NOT start it. If unreachable, stop and tell the user.
2. bee is configured to reach it. Confirm with one cheap probe before a full run:
   `bee run --headless --provider omlx "say ok"` (or pass `--provider/--model` to
   `bee bench`).

## The loop

1. **Baseline.** `bee bench --label baseline --json` → note the results path.
   Read it. Record aggregate + per-dimension means (success / format / efficiency).

2. **Diagnose.** Find the worst dimension, then the worst tasks within it.
   - low **success** → tasks not getting done. Read the failing task's transcript;
     is it the prompt, a missing tool in the surface, or giving up early?
   - low **format** → errored/malformed tool calls, hallucinated tool names, or
     not stopping cleanly. Usually a system-prompt or tool-description problem.
   - low **efficiency** → thrash: too many turns/tokens for the result.

3. **Hypothesize ONE tweak.** Exactly one variable. Candidates:
   - system-prompt wording (`internal/prompt/…`)
   - tiny-profile budgets / tool surface (`internal/config/defaults.go`, `scale.go`)
   - a tool's description/spec
   - caveman level for the profile

4. **Apply + re-measure.** `bee bench --label <tweak-tag> --json`.

5. **Compare.** Diff aggregate + per-task vs the current best. Keep if up, revert
   if down or flat. Never keep a change that lowers a dimension to buy another
   unless the aggregate clearly wins.

6. **Journal.** Append one entry to `bench/journal/<date>.md`:
   `tweak-tag | what changed | aggregate Δ | per-dim Δ | kept|reverted | note`.

7. Repeat from step 2 until the user stops you or several rounds yield no gain.

## Guardrails

- **Never edit the task suite to raise the score** — that is overfitting and
  invalidates the benchmark. Only tune bee knobs.
- **One change per iteration.** Bundled changes make deltas un-attributable.
- **Always journal**, including reverts — the journal is the audit trail and lets
  a later session resume without re-deriving what was tried.
- Small models are noisy. If a delta is within run-to-run noise, re-run before
  trusting it; treat tiny aggregate moves (<~2 pts on a small suite) as flat.

## Reading a result file

```
{ "label", "aggregate", "dim_means": {success,format,efficiency},
  "tasks": [ { "id","score","dims","succeeded","metrics","checks","reason" } ] }
```

`metrics` (turns / tool_calls / errored_calls / stopped_clean) is where the
behavioral signal lives — lean on it to form the hypothesis, not just the score.

## Run ledger

Every `bee bench` run auto-appends one JSON line to `bench/results/ledger.jsonl`
(time, label, provider, model, profile, suite, tasks, runs, aggregate, per-dim
means, holdout, results path). This is the durable history of ALL runs, including
ad-hoc model comparisons. Consult it to compare across runs and models without
reopening each results JSON, and never delete or rewrite it. The `bench/journal/`
entries record tuning DECISIONS; the ledger records raw measurements. Disable per
run only with `--ledger ""`, and only with reason.
