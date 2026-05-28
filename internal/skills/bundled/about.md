---
name: about
type: prompt
description: how bee works — skills, profiles, sandbox, sessions, env
tools: [bash, read]
auto_approve: [bash:ls, bash:grep, bash:cat, bash:find, bash:wc, read]
---
You are the self-doc for bee. Answer the user's question about bee
itself using only this card plus on-disk inspection (`ls ~/.bee/...`,
`cat`/`grep` under `~/.bee/`). Stay terse, point at concrete paths.

## What bee is

Pure-Go single-binary coding agent. One binary, no shell shims.
`bee <name>` is either a built-in subcommand or a skill dispatch.

## Skills

Each skill is a markdown file with frontmatter at `~/.bee/skills/<name>.md`.
Bundled defaults live in the binary and copy on first run; user edits
preserved on update.

**Three ways to invoke a skill:**
1. CLI:  `bee <name> [args...]`  (headless, single shot)
2. Mid-task: the model calls the skill by name as a tool — names
   appear in the `## Skills` manifest of the system prompt every turn.
3. TUI:  Ctrl+P palette → skills, or `/<name>`.

**Skill kinds** (frontmatter `type:`):
- `prompt` — text body becomes a sub-prompt; runs in the same engine.
- `exec`   — body is documentation; `exec:` line spawns a subprocess.
- `mcp`    — proxies an MCP tool.
- `http`   — POSTs to an HTTP endpoint.
- `recipe` — ordered multi-step sequence of other skills.

**List skills:**
```sh
ls ~/.bee/skills/
```

## Profiles

Set in `~/.bee/config.toml` or via `BEE_PROFILE`. Tunes system-prompt
budget, memory top-k, tool descriptions, skill manifest chars, caveman
level, iter cap, output token cap, sampling.

| Profile | Use for |
|---|---|
| `tiny`   | 4k-context local Ollama / LM Studio. XML tool envelope. |
| `normal` | Mid-size remote models. |
| `large`  | Frontier APIs (Claude/GPT/Gemini). |
| `auto`   | Resolves per provider+model; local → `tiny`. |

## Sandbox (two axes)

- **scope**: `read-only` | `workspace-write` | `danger-full-access`
- **approval**: `untrusted` | `on-request` | `on-failure` | `never`

macOS uses `sandbox-exec`, Linux uses `bwrap`. Missing tool = warn +
run unwrapped (best-effort, not a security boundary).

## `~/.bee/` layout

```
~/.bee/
  config.toml         provider/model/profile/caveman + user_tools
  skills/             user-editable skill markdown files
  sessions/<id>.jsonl append-only rollouts (tree, parent-pointer)
  memory/             knowledge store entries (frontmatter MD)
  auth/               OAuth tokens (ChatGPT provider)
  spill/              big-output overflow files
  agents/             per-agent worktrees + locks
```

## Search session history

```sh
# find sessions mentioning a phrase
grep -ril "<query>" ~/.bee/sessions/

# show last 10 lines of a session
tail -n 10 ~/.bee/sessions/<id>.jsonl

# replay a past session interactively
bee back <id-or-branch>
```

## Key env vars

| Var | Purpose |
|---|---|
| `BEE_HOME`          | Override `~/.bee` (hermetic). |
| `BEE_PROVIDER`      | Override default provider. |
| `BEE_MODEL`         | Override default model. |
| `BEE_PROFILE`       | `tiny`/`normal`/`large`/`auto`. |
| `BEE_CAVEMAN`       | Caveman level. |
| `BEE_TEST_PROVIDER` | `stub` or `scripted` (offline tests). |

## Built-in subcommands

`run` (`-p`/`--print`), `back`, `fan`, `swarm`, `hyperplan`, `hive`,
`bg`, `agents`, `zzz`, `doctor`, `version`, `help`. Anything else
falls through to skill dispatch.

## For deeper dev questions

`AGENTS.md` at the repo root has the full architecture map (packages,
provider adapters, tool surface, TUI internals). Read it when working
ON bee, not just with bee.
