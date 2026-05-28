---
name: session
type: prompt
description: read another bee session's history by id; locate `.jsonl`, summarize messages, then continue the task in the current session
tools: [bash, read]
auto_approve: [bash:ls, bash:cat, bash:tail, bash:head, bash:grep, bash:wc, bash:jq, read]
---
You inspect another bee session by id and surface its content into the
current conversation. Use this when the user says "continue session X",
"what did we do in X", "load session X", or passes a bare uuid.

## Where sessions live

```
~/.bee/sessions/<id>.jsonl
```

One JSON message per line. Schema: `{role, content:[{type,text|...}], ...}`.
Override with `BEE_SESSIONS_DIR` if set; check env first.

## Resolve the id

1. Full uuid → use as-is.
2. Prefix → match against `ls ~/.bee/sessions/`:
   ```sh
   ls ~/.bee/sessions/ | grep '^<prefix>'
   ```
3. `latest` / `l` → newest by mtime:
   ```sh
   ls -t ~/.bee/sessions/*.jsonl | head -1
   ```
4. Fuzzy phrase → `grep -ril "<phrase>" ~/.bee/sessions/`.

If multiple match, list candidates with first user line as preview; ask
which one.

## Read the history

```sh
# count messages
wc -l ~/.bee/sessions/<id>.jsonl

# last 5 messages, role + text only
tail -n 5 ~/.bee/sessions/<id>.jsonl \
  | jq -r '"\(.role): \(.content[0].text // .content[0].type // "?")"'

# full extract, role-tagged
jq -r '"--- \(.role) ---\n\(.content[]? | .text // "")"' \
  ~/.bee/sessions/<id>.jsonl
```

For long sessions, summarize: first user prompt, last assistant turn,
any pending tool calls, open todos.

## Continue the task

After reading: state what the prior session was doing and what's next,
then continue in the current session. The current process *is* a new
session — you cannot retroactively merge rollouts. New work lands here.

If the user wants to literally resume (same scrollback, same tree
position), tell them to exit and run:

```sh
bee back <id>
```

That re-execs into the prior rollout in append mode. You can't do it
mid-turn.

## Anti-patterns

- Don't guess the path — always check `~/.bee/sessions/` exists and
  the file is `.jsonl` (not `.json`).
- Don't dump the whole jsonl into the conversation; summarize.
- Don't claim you "resumed" the session. You read it. Different thing.
