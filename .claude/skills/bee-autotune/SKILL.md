---
name: bee-autotune
description: Autonomously tune bee's small-model behavior in an unattended loop. Use when the user wants bee to self-improve its bench score against a local model without hand-holding — sweep config knobs, keep wins, revert losses by score, journal, stop on target/plateau. Distinct from bee-tune (interactive, one tweak at a time): this runs the loop end to end and only reports at the end.
---

# bee-autotune — unattended improvement loop

`bee bench` measures. `bee-tune` is the interactive loop. This is the **autonomous**
loop: pick a knob, measure, keep/revert by score band, journal, repeat until a stop
condition — no prompting between iterations.

Two levers, in order of preference:
1. **Config overlay** (no recompile) — flip one profile knob via a TOML overlay.
2. **Source edit + rebuild** — only when a hypothesis isn't config-expressible.

## How overlays work (verified)

`BEE_CONFIG=<path>` repoints the whole config file. go-toml **merges per-key**: an
overlay with just

```toml
[profiles.tiny]
caveman = "full"
```

overrides *only* that field — tiny's other fields and the normal/large profiles stay
intact. So a candidate overlay is the running best plus exactly one changed knob.

`bee bench` passes it through: `--config <overlay.toml> --profile tiny`. The bench
never touches `~/.bee/config.toml`; the overlay is the only mutation surface.

Override the **tiny** profile by name (not a new profile name) — some tool-surface
logic keys off the literal profile name `tiny`, so a renamed profile behaves
differently. Keep the name, change the fields.

## Preconditions

1. The local server (omlx) is already serving the target model. This skill does not
   start it. One cheap probe first: `bee run --headless --provider omlx "say ok"`.
2. If you edited Go source, rebuild and drive the fresh binary:
   `go build -o /tmp/bee-tune ./cmd/bee` then run `/tmp/bee-tune bench …`
   (`bee bench` spawns its own executable, so the binary you invoke is the one under
   test). For pure config sweeps the installed `bee` is fine if current.

## The loop

1. **Baseline.** `bee bench --runs 3 --label baseline --json`. Read the JSON. Record
   aggregate `A0`, per-dim means, and `mean_spread` `S0`.
   - **Noise band** `B = max(2.0, S0)`. Deltas inside `B` are noise, full stop.
   - `Abest = A0`. Seed `bench/sweep/best.toml` empty (no overrides yet).

2. **Pick one knob** aimed at the worst dimension (see sweep space). Don't repeat a
   knob+value already journaled.

3. **Write candidate.** Copy `best.toml` to `bench/sweep/<tag>.toml`, add/replace the
   single knob under `[profiles.tiny]`. `<tag>` names the knob+value, e.g.
   `caveman-full`, `temp-0.3`, `sysbudget-5000`.

4. **Measure.** `bee bench --config bench/sweep/<tag>.toml --profile tiny --runs 3 --label <tag> --json`.

5. **Keep/revert** (automatic, no prompt):
   - **Keep** if `Av - Abest > B`: promote — `cp bench/sweep/<tag>.toml bench/sweep/best.toml`, set `Abest = Av`.
   - **Revert** otherwise: discard the candidate (leave `best.toml` as is).

6. **Journal** every iteration (incl. reverts) to `bench/journal/<date>.md`:
   `tag | knob=value | A before→after | Δdims | kept|reverted | one-line why`.

7. **Loop** to step 2 until a stop condition.

## Sweep space (prioritized, all config-expressible)

Target the worst dimension first.

- low **success** → `system_prompt_budget` up (more room for discipline rules);
  `max_iterations` up (don't cap before done); `read_default_lines` up (enough file
  context to act); for multi-file/refactor tasks, `tool_output_tokens` up.
- low **format** → `caveman` level (try `full` then `lite` — over-compression can
  garble tool JSON); `tool_format` (`xml` vs native) for models that malform
  tool_calls; `tool_desc_chars` up so tool contracts aren't truncated.
- low **efficiency** → `caveman` toward `ultra` (fewer tokens/turn); `max_iterations`
  down (force decisiveness); `grep_max_matches`/`read_default_lines` down (less
  thrash). Watch success doesn't drop to buy efficiency.
- sampling: `temperature` (0.0–0.4) and `top_p` — small moves; high temp raises
  format errors, very low can loop.

One knob per iteration — bundled changes make deltas un-attributable.

## Escalating to source

If the best hypothesis can't be expressed as a config knob (system-prompt wording in
`internal/prompt/…`, eval protocol in `internal/goal/eval.go`, tool descriptions,
scoring), edit it, `go build -o /tmp/bee-tune ./cmd/bee`, and measure with that
binary. These are rarer and riskier — keep them surgical, still one change, still
journaled, and still subject to the same keep/revert band.

## Stop conditions

Stop and report when any holds:
- **Target met** — aggregate ≥ the user's target (default: `A0 * 1.10`, +10%).
- **Plateau** — `K = 3` consecutive iterations with no keep.
- **Cap** — `12` iterations total this session.

On stop, surface: baseline→final aggregate and per-dim, the winning `best.toml`
(the exact knobs that won), and a one-line note per kept change. State plainly if it
plateaued below target — don't dress up a flat result.

## Guardrails

- **Never edit `bench/tasks/` or `bench/fixtures/`** to raise the score — that is
  overfitting and invalidates the benchmark. Tune bee only.
- **One change per iteration.** Always re-measure with `--runs 3`; never trust a
  single shot.
- **Leave `~/.bee/config.toml` untouched.** All sweeps go through `--config`
  overlays under `bench/sweep/`. Adopting the winner permanently (folding `best.toml`
  into `~/.bee/config.toml` or the Go defaults) is a separate, explicit step the user
  approves — not part of the loop.
- **Journal everything**, including reverts and source escalations — it is the audit
  trail and lets a later session resume without re-deriving what was tried.
- **The run ledger is automatic.** Every `bee bench` appends a row to
  `bench/results/ledger.jsonl` (raw measurements of every run). Leave it on (don't
  pass `--ledger ""`); it is the cross-run history the journal's decisions reference.
