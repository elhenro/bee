# bee fine-tune track (MLX LoRA)

Close the improvement loop around the *model*: harvest the runs the bench already
blessed, LoRA fine-tune the local model on them, serve the result through omlx, and
score it on the **same** bench. The adapter competes with the base model on identical
terms.

## Honest expectations

This is behaviour cloning from a small task suite. The pipeline is real and runs end
to end, but the data is thin — a handful of clean rollouts will not move a 35B model
much. Treat this as the scaffold. The levers that actually decide whether a fine-tune
helps:

- **Corpus size** — rejection-sample: run the suite many times (higher temperature),
  keep only clean wins (`--rollouts` already filters to passed + no errored calls +
  clean stop). Hundreds of demonstrations, not dozens.
- **Format fidelity** — `harvest.py` renders tool calls as text and omits bee's
  runtime system prompt (it is assembled at run time, not stored in the session).
  Matching bee's exact tool wire-format and prepending the assembled system prompt to
  each record is what makes the cloned behaviour transfer back into the harness.

Journal the delta with this caveat. Do not claim a win the data cannot support.

## Pipeline

Tooling lives in a local venv (`bench/finetune/.venv`, gitignored). `mlx` core ships
with the machine; `mlx-lm` is installed here.

```sh
# 0. tooling (once)
python3 -m venv bench/finetune/.venv
bench/finetune/.venv/bin/pip install mlx-lm

# 1. harvest a corpus — run the suite with --rollouts, repeat to grow it
OMLX_API_KEY=… bee bench --provider omlx --model Qwen3.6-35B-A3B-UD-MLX-4bit \
    --runs 5 --rollouts bench/finetune/rollouts
bench/finetune/.venv/bin/python bench/finetune/harvest.py \
    --in bench/finetune/rollouts --out-dir bench/finetune/data

# 2. LoRA train (base model dir served by omlx under ~/.omlx/models)
bench/finetune/.venv/bin/mlx_lm.lora \
    --model ~/.omlx/models/Qwen3.6-35B-A3B-UD-MLX-4bit \
    --train --data bench/finetune/data \
    --adapter-path bench/finetune/adapters \
    --iters 200 --batch-size 1

# 3. fuse adapter into a standalone model dir
bench/finetune/.venv/bin/mlx_lm.fuse \
    --model ~/.omlx/models/Qwen3.6-35B-A3B-UD-MLX-4bit \
    --adapter-path bench/finetune/adapters \
    --save-path ~/.omlx/models/Qwen3.6-35B-A3B-bee-lora

# 4. register with omlx so it serves the fused model
#    add an entry for the new dir to ~/.omlx/model_settings.json, then restart omlx.

# 5. benchmark the adapter against base — same harness, same scoreboard
OMLX_API_KEY=… bee bench --provider omlx --model Qwen3.6-35B-A3B-bee-lora \
    --runs 3 --label lora-v1
```

## Files

- `harvest.py` — rollout jsonl → MLX chat `train.jsonl`/`valid.jsonl`.
- `rollouts/` — blessed session jsonl from `bee bench --rollouts` (regenerable).
- `data/` — harvested MLX training data (gitignored).
- `adapters/` — LoRA weights (gitignored).
- `.venv/` — mlx-lm toolchain (gitignored).

Harness vs model tuning share one scoreboard: tune knobs with `bee-autotune`, tune
weights here, and compare both as labelled rows in `bench/results/`.
