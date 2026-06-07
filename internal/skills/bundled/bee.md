---
name: bee
type: prompt
description: operate bee on itself — search session/history, inspect or change config, rebuild the binary, run doctor, locate state. invoke whenever the user asks bee about its own sessions, config, build, skills, checkpoints, usage, or "did we lose X".
tools: [bash, read, grep, find, ls]
auto_approve: [bash:ls, bash:cat, bash:tail, bash:head, bash:grep, bash:rg, bash:jq, bash:wc, bash:find, bash:tree, bash:git, read, grep, find, ls]
---
You operate bee on itself. Use this for any self-referential request:
search past sessions, recover lost work, inspect/change config, rebuild,
check health, find state. Be safe: read freely, mutate carefully.

## State layout

```
~/.bee/
  sessions/<uuid>.jsonl   one JSON message per line — the rollouts
  history                 flat input history (all typed prompts)
  config.toml             user config (secrets live here — never echo raw)
  auth/<provider>.key     api keys — NEVER read, print, or stage
  skills/*.md             user skills (this file lives here)
  checkpoints/            session checkpoints (/checkpoint, /rewind)
  spill/                  large-output overflow blobs
  usage.jsonl            per-turn token/cost log
  cmd_usage.json         per-command counters
  lifetime_tokens.json   running token total
  agents/ waggle/ zzz/    background-agent + scheduler state
```

Env overrides (check before assuming paths):
`BEE_CONFIG`, `BEE_SESSIONS_DIR`, `BEE_PROVIDER`, `BEE_MODEL`, `BEE_ROLE`,
`BEE_PROFILE`, `BEE_EFFORT`, `BEE_YOLO`.

## Search session / history

For one session by id, hand off to the `session` skill. For *across all*
sessions ("did we lose /docs", "where did we do X", "find that PR"):

```sh
# fuzzy hit across every rollout, filenames only
grep -ril "<phrase>" ~/.bee/sessions/ | head

# every prompt ever typed, with context
grep -in "<phrase>" ~/.bee/history

# rank sessions by hit count
grep -ric "<phrase>" ~/.bee/sessions/ | grep -v ':0$' | sort -t: -k2 -rn | head

# extract role-tagged text from a matched session
jq -r '"--- \(.role) ---\n\(.content[]? | .text // "")"' ~/.bee/sessions/<id>.jsonl
```

Newest sessions first: `ls -t ~/.bee/sessions/*.jsonl | head`.
Don't dump whole jsonl into the chat — grep, slice, summarize.

## Inspect config

Resolution order: **defaults < ~/.bee/config.toml < env vars**.
What's on disk isn't the whole story — env can override.

```sh
# on-disk config, secrets blanked
sed -E 's/(key|token|secret)[[:space:]]*=.*/\1 = <redacted>/I' ~/.bee/config.toml

# what bee actually resolves (provider/model/role health)
bee doctor

# active env overrides
env | grep '^BEE_'
```

Never print `~/.bee/auth/*` or raw secret lines from config.toml.

## Change config

Two paths. Prefer live slash commands over hand-editing — they validate
and persist:

| Want                | In-session                         | Persisted file key      |
|---------------------|------------------------------------|-------------------------|
| model               | `/model`, `/model <prov>/<id>`     | `default_model` + `default_provider` |
| role                | `/role <worker\|scout\|queen>`     | `role`                  |
| reasoning budget    | `/effort <off..max\|auto>`         | `thinking`              |
| tool-use cap        | `/iterations <n>` (0 = unlimited)  | `max_iterations`        |
| verbosity / thoughts| `/settings <key> <on\|off>`        | `verbose`, `show_*`     |

Hand-edit `~/.bee/config.toml` only for keys with no command (compaction
threshold, sandbox allowlist, vision model). It's TOML — keep types.
Back up first: `cp ~/.bee/config.toml ~/.bee/config.toml.bak`.

## Rebuild

bee is pure Go, built from `./cmd/bee`. Binary currently at the path
`which bee` reports (commonly `~/.local/bin/bee`).

```sh
cd <bee repo>            # module github.com/elhenro/bee
go build -o "$(which bee)" ./cmd/bee
bee version              # confirm it swapped
```

Test before declaring done — `go build ./... && go test ./...`.
Don't trust "compiles" as "works"; run `bee doctor`.

## Health / debug

- `bee doctor` — provider reachability, model presence, config sanity.
- `bee waggle ls` / `bee agents` — background + scheduled agent state.
- Stuck small model spinning multi-file work → suggest a model switch or a
  tighter profile; don't just wait it out.

## Anti-patterns

- Reading/printing/staging `~/.bee/auth/*` or raw secrets. Always redact.
- Editing config.toml with no backup, or breaking TOML types.
- Dumping a full jsonl rollout into the conversation — summarize.
- Claiming a rebuild "works" from a clean compile. Run it.
- Guessing paths when `BEE_*` env vars may be redirecting them.
