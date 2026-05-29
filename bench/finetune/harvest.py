#!/usr/bin/env python3
"""harvest blessed bench rollouts into MLX-LM chat training data.

input  : a dir of session jsonl files written by `bee bench --rollouts <dir>`
         (one types.Message per line; bench only persists passed + clean runs).
output : train.jsonl / valid.jsonl in MLX chat format, one record per rollout:
         {"messages": [{"role": ..., "content": ...}, ...]}

this is behaviour cloning of clean runs. note the format is an approximation:
tool calls are rendered as text, and bee's runtime system prompt is not in the
session file, so it is not in the record. matching bee's exact tool wire-format
and prepending the assembled system prompt are the refinements that decide
whether a fine-tune actually moves the needle. see README.
"""

import argparse
import json
import pathlib
import sys


def render_tool_use(use: dict) -> str:
    name = use.get("name", "?")
    inp = use.get("input", {})
    pairs = " ".join(f"{k}={v}" for k, v in sorted(inp.items()))
    return f"[tool_call {name} {pairs}]".strip()


def message_to_records(msg: dict) -> list[dict]:
    """one session message -> zero or more chat records, order preserved.

    thinking blocks are dropped (train on the action, not the scratchpad).
    tool_result blocks become role "tool"; tool_use is folded into the
    assistant turn so the model learns call placement.
    """
    role = msg.get("role", "")
    blocks = msg.get("content") or []
    if msg.get("ephemeral"):
        return []

    text_parts, tool_calls, tool_results = [], [], []
    for b in blocks:
        t = b.get("type")
        if t == "text" and b.get("text"):
            text_parts.append(b["text"].strip())
        elif t == "tool_use" and b.get("tool_use"):
            tool_calls.append(render_tool_use(b["tool_use"]))
        elif t == "tool_result" and b.get("tool_result"):
            tr = b["tool_result"]
            prefix = "[error] " if tr.get("is_error") else ""
            tool_results.append(prefix + (tr.get("content") or "").strip())
        # image / thinking: skip

    out = []
    if role == "assistant":
        content = "\n".join([p for p in text_parts if p] + tool_calls).strip()
        if content:
            out.append({"role": "assistant", "content": content})
    elif role in ("user", "system"):
        for tr in tool_results:  # results sometimes ride a user turn
            out.append({"role": "tool", "content": tr})
        joined = "\n".join(p for p in text_parts if p).strip()
        if joined:
            out.append({"role": role, "content": joined})
    elif role == "tool":
        for tr in tool_results:
            out.append({"role": "tool", "content": tr})
    return out


def rollout_to_record(path: pathlib.Path) -> dict | None:
    messages = []
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(msg, dict) and "role" in msg:
            messages.extend(message_to_records(msg))
    # need at least one user prompt and one assistant turn to be trainable
    roles = {m["role"] for m in messages}
    if "assistant" not in roles or "user" not in roles:
        return None
    return {"messages": messages}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="indir", required=True, help="rollout jsonl dir")
    ap.add_argument("--out-dir", default="bench/finetune/data", help="MLX data dir")
    ap.add_argument("--val-frac", type=float, default=0.2)
    args = ap.parse_args()

    indir = pathlib.Path(args.indir)
    files = sorted(indir.glob("*.jsonl"))
    if not files:
        print(f"no rollouts in {indir}", file=sys.stderr)
        return 1

    records = [r for f in files if (r := rollout_to_record(f))]
    if not records:
        print("no trainable records harvested", file=sys.stderr)
        return 1

    # deterministic split: every Nth record to validation, rest to train.
    step = max(2, int(1 / args.val_frac)) if args.val_frac > 0 else 0
    train, valid = [], []
    for i, r in enumerate(records):
        (valid if step and i % step == 0 else train).append(r)
    if not valid:  # tiny corpus: borrow one
        valid = train[:1]

    outdir = pathlib.Path(args.out_dir)
    outdir.mkdir(parents=True, exist_ok=True)
    for name, rows in (("train", train), ("valid", valid)):
        p = outdir / f"{name}.jsonl"
        p.write_text("".join(json.dumps(r) + "\n" for r in rows))
        print(f"{name}: {len(rows)} records -> {p}")
    print(f"harvested {len(records)} blessed rollouts from {len(files)} files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
