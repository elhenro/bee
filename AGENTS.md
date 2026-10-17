# AGENTS.md

Guidance for agents (and humans) working on this repository. Read this **before** any non-trivial change.

## Project

`bee` is a pure-Go single-binary coding agent. Three intentional wedges over other CLI coding agents:

1. **Skills are `bee <name>` subcommands.** `~/.bee/skills/<name>.md` is invokable as `bee <name> [args...]` — one binary, one PATH entry, no shell shims sprayed onto `$PATH`. Unknown `arg[1]` falls through to skill-registry lookup via `dispatchSkill`.
2. **Skills are agent endpoints.** Three kinds: `prompt` | `exec` | `recipe`. The same skill is surfaced both as a `bee <name>` subcommand AND a model-callable tool the agent can invoke mid-task (exec only; prompt/recipe fold into the user turn). A recipe whose every step has a `kind` (`tool` | `shell` | `bee`) is a **runtime recipe**: `bee <name>` executes it in Go (`internal/recipe/`), deterministic steps cost no tokens, and only `bee` steps spawn a model turn.
3. **Tiny-context friendly.** System-prompt budget is configurable per-profile (`tiny|normal|large|auto`); memory injection is lazy top-k; tool descriptions and skill manifest are token-budgeted. Designed to run against a 4k-context local Ollama as well as frontier APIs.

Other load-bearing choices: `apply_patch` collapses write/edit/multi-edit on capable models (tiny profile swaps it out for `write`+`edit`+`hashline_edit`); codex-style two-axis sandbox; frontmatter knowledge store with lazy top-K selection; textmode wrapper that emits XML-style tool calls for local models that ignore `tool_calls`.

## Local LLMs

`bee` is designed to work well with local LLMs. Key features:

- **Text mode** (`ToolFormat="xml"` in profile): injects an XML-style tool advert into the system prompt and parses `<tool>{...}</tool>` envelopes out of the assistant content stream. Designed for tiny/local models that ignore `tool_calls`.
- **Profile auto-detection**: local providers (`ollama`/`lmstudio`) always resolve to the `tiny` profile.
- **Caveman rules**: prompt-injection compression rules (`rules/{full,lite,ultra}.md`) that compress bee's responses without affecting user input.
- **Stub provider**: `BEE_TEST_PROVIDER=stub` for offline development and testing.
- **4k-context friendly**: the whole system is designed to run on a 4k-context local Ollama as well as frontier APIs.

## Commands

```sh
go build ./...                                # full build (must stay clean)
go build -o ~/.local/bin/bee ./cmd/bee        # install local
go test ./...                                 # all tests
go test ./internal/<pkg>/... -run TestName    # single package or test
go vet ./...                                  # vet (must stay clean)
golangci-lint run                             # uses .golangci.yml (govet/errcheck/staticcheck/ineffassign/unused/misspell)
```

End-to-end smoke without network (used by CI and during development):

```sh
BEE_TEST_PROVIDER=stub ./bee run --headless "anything"
BEE_TEST_PROVIDER=scripted BEE_TEST_SCRIPT=<fixture.jsonl> ./bee run "..."
```

Real OpenRouter smoke (requires `OPENROUTER_API_KEY`):

```sh
./bee run "say hi in three words"
BEE_PROVIDER=anthropic BEE_MODEL=claude-sonnet-4-5 ./bee run "say hi in three words"   # override provider/model inline
```

First-run is implicit: `bee run` / `bee` / `bee <skill>` all call `ensureFirstRun`, which creates `~/.bee/skills` and drops the bundled skills the first time it sees an empty dir. User edits are preserved on subsequent installs.

Override `$HOME` via `BEE_HOME=/tmp/iso` for hermetic install tests.

## Subcommands

`cmd/bee/main.go` dispatches a small fixed set; everything else falls through to skill lookup.

| Command | What it does |
|---|---|
| `run` / `-p` / `--print` | Headless single-shot run. Engine + stdout, no TUI. |
| *(none)* | TUI (`tui.go`). Same Engine wiring as `run`. |
| `back` | Re-enter a previous session by id or tree branch — replays history. |
| `fan` | N independent engines, same prompt, parallel. |
| `swarm` | Planner decomposes → worker pool executes → planner synthesizes. |
| `hyperplan` | 5 critic engines + 1 synthesizer queen over a plan draft. |
| `hive` | Long-running multi-bee pool view (same runtime as `swarm`/`fan`). |
| `bg` | Re-exec headless with a pinned session id, detached via `Setsid`. |
| `agents` | TUI overview of parallel detached agents — each chat submit spawns one in its own worktree. |
| `remote-control` | Serves a local web relay (URL + QR) to drive bee from another device over the LAN. |
| `zzz` | Overnight autonomous-commit loop. Same engine, sentinel-driven stop. |
| `cron` | `ls` / `run <name>` / `daemon` over `[[cron.jobs]]` in config. Jobs run through the shell and take the zzz worktree lock, so cron and `zzz` never drive one tree at once. |
| `doctor` | Read-only preflight: provider keys, sandbox tools, ollama probe, models cache. |
| `bench` | Small-model benchmark harness — scored task suite with ledger + holdout split. |
| `version` / `-v` / `--version` | Build version. |
| `help` / `-h` / `--help` | Usage. |
| *anything else* | `dispatchSkill(arg[1], rest...)` → headless run with `--skill <name>`. Falls back to the waggle stores (`waggle_skill.go`): an exec waggle follows directly, a recipe waggle runs through the recipe executor. |

`stub_provider.go` is gated by `BEE_TEST_PROVIDER=stub` (or `scripted` for fixture-driven runs) so the binary works offline in tests.

## Architecture

Clean **types → provider → tools → loop → ui** stack. Internal packages talk via the interfaces in `internal/types`, `internal/llm/provider.go`, `internal/tools/registry.go`. Implementations stay swappable.

- **`cmd/bee/`** — entry + subcommand wiring. `main.go` is a stdlib switch (see table above). `run.go` is the headless engine path; `run_tools.go` builds the tool registry (with optional `writeRe` path filter for confined runs and an `approval.Approver` for dangerous-command gating); `run_provider.go` resolves provider/model from config + env. `tui.go` wires the same Engine into the Bubbletea app. `fan.go`/`swarm.go`/`hyperplan.go`/`hive.go` build N engines for multi-bee work. `bg.go` daemonizes; `agents.go` opens the parallel-agents pane; `zzz.go` runs the overnight loop. `doctor.go` is the preflight. `stub_provider.go` is gated by `BEE_TEST_PROVIDER`.

- **`internal/loop/`** — the agent turn loop. `Engine.Run(ctx, userMsg)` selects knowledge entries → assembles system prompt → streams provider events → dispatches tool calls serially → folds results → recurses. Hard `MaxIterations` cap (config + profile override). `Role ∈ {worker, scout, queen}` (`role.go`): worker = full surface with a per-turn read|act classifier (a read-only turn uses the `readOnlyTools` whitelist), scout = read-only research + web (`web_search`/`web_fetch`), queen = spawns a hive (TUI only). Each role bakes its own reasoning budget (`RoleThinking`); `yolo` is a separate auto-approve toggle, not a role. Legacy `mode`/`mastermind` config keys migrate forward on load (`config/load.go`). `compact.go` summarizes mid-history when context fills; `recap.go` produces post-turn end-of-task recaps when enabled. `Engine.PromptTail` is a per-iteration hook whose text rides the volatile system-prompt suffix (after the prompt-cache breakpoint, see `prompt_tail.go`); the hive uses it for the pheromone card. `handoff.go` builds the rescue brief for a model switch and the hive's death report (`BuildDeathReport`) from the same distillation path. `done_signal.go` + sentinel markers let unattended loops detect "I'm done". `sandbox_wrap.go` wraps shell calls with the active sandbox policy. `KnowledgeStore` is an interface so the loop never imports `internal/knowledge` directly.

- **`internal/llm/`** — `Provider` interface + adapters. Built-ins:
  - `openai_compat.go` — OpenRouter / OpenAI / DeepSeek / Groq / Ollama / LM Studio via `base_url + wire_api=chat`. Streaming in `openai_compat_stream.go`; stall-watchdog in `openai_compat_stall_test.go`.
  - `claude.go`/`anthropic.go` — native Anthropic Messages API (`wire_api=anthropic-messages`), streaming in `claude_stream.go`, thinking-block aware.
  - `chatgpt.go` — OAuth-backed ChatGPT account via `internal/auth` (`wire_api=responses`); request/stream split across `chatgpt_request.go`/`chatgpt_stream.go`, with `chatgpt_auth.go`/`chatgpt_models.go` for token + model handling.
  - `gemini.go` — native Google Gemini (`wire_api=gemini`).
  - `textmode.go` — wraps another `Provider`, injects an XML-style tool advert into the system prompt and parses `<tool>{...}</tool>` envelopes out of the assistant content stream. Opt-in per profile via `ToolFormat="xml"` for tiny/local models that ignore `tool_calls`. Parser in `textmode_parse.go`.
  - `thinking_hybrid.go` — handles providers that emit reasoning in a side channel vs. inline `<thinking>` tags.
  - `models.go` + `models_cache.go` + `models_hardcoded.go` — model registry with on-disk cache; pricing fuels `internal/cost`.
  - `wire/` — translates internal `types.Message`/`ToolUse`/`ToolResult` to/from each provider's wire format: `openai.go`/`openai_stream.go`, `anthropic_messages.go`/`anthropic_messages_stream.go`, `responses.go`/`responses_stream.go`, `gemini.go`. **Internal message types are agent-owned — never leak provider SDK types upward.**
  - `mockprov/` — fixture-driven `Provider` for scripted e2e tests.

### Providers

Built-in provider blocks ship in `internal/config/defaults.go` (`Providers` map); override per-project in `~/.bee/config.toml`. The env-var in the `EnvKey` column is read at startup unless `KeyOptional=true`.

| Provider | Base URL | Wire API | Env Key | Auth |
|---|---|---|---|---|
| `openrouter` | `https://openrouter.ai/api/v1` | `chat` | `OPENROUTER_API_KEY` | key |
| `openai` | `https://api.openai.com/v1` | `chat` | `OPENAI_API_KEY` | key |
| `anthropic` | `https://api.anthropic.com/v1` | `anthropic-messages` | `ANTHROPIC_API_KEY` | key |
| `gemini` | `https://generativelanguage.googleapis.com/v1beta` | `gemini` | `GEMINI_API_KEY` | key |
| `ollama` | `http://localhost:11434/v1` | `chat` | — | none (local) |
| `omlx` | `http://localhost:8000/v1` | `chat` | `OMLX_API_KEY` | key (optional) |
| `chatgpt` | `https://chatgpt.com/backend-api/codex` | `responses` | — | OAuth (`/login chatgpt`) |

Any new OpenAI-compatible endpoint (DeepSeek, Groq, Together, custom proxy, …) is a config-only addition — no code, just a `[providers.<name>]` block:

```toml
default_provider = "openrouter"
default_model    = "anthropic/claude-sonnet-4.5"

[providers.openrouter]
base_url      = "https://openrouter.ai/api/v1"
wire_api      = "chat"
env_key       = "OPENROUTER_API_KEY"
default_model = "anthropic/claude-sonnet-4.5"
reports_cost  = true

[providers.deepseek]
base_url      = "https://api.deepseek.com/v1"
wire_api      = "chat"
env_key       = "DEEPSEEK_API_KEY"
default_model = "deepseek-chat"
```

- **`internal/tools/`** — current surface (Spec name in `tools/<dir>/<dir>.go`):
  - **Read-side**: `read`, `search` (regex grep, code in `internal/tools/grep/`), `glob` (filename match, code in `internal/tools/find/`), `ls`, `godoc` (`go doc -short` for a package/symbol), `codegraph` (symbol-relationship queries over the project's CodeGraph index).
  - **Web**: `web_search` (Brave Search API, top-5 results), `web_fetch` (fetch + extract a URL).
  - **Write-side**: `apply_patch` (unified-diff multi-edit; tiny profile skips it), `write`, `edit` (search-and-replace, code in `internal/tools/edit_diff/`), `hashline_edit` (line-number based, robust on tiny models).
  - **Shell**: `bash` (code in `internal/tools/shell/`, wrapped by sandbox policy + `internal/approval` for dangerous-command gating).
  - **Knowledge**: `knowledge_search`, `knowledge_write` — frontend to `internal/knowledge`. Disabled when `[memory] enabled=false`.
  - **Procedure memory**: `waggle_lookup` — list or run a crystallized read-only route (waggle) on demand. Registered only when the library is non-empty and `[waggle] enabled` (default on); see `internal/waggle/`.
  - **Meta**: `tool_lookup` — model-callable "what tools do I have, and how do I use them?" Reads back from the registry so it always sees the live filtered surface, including user-defined tools. `ask_user` — pose a multiple-choice question and block on the user's pick (auto-resolves headless via `internal/ask`). `escalate` — signal "stuck, same approach failed repeatedly" for unattended-loop handling.
  - **Config-defined**: `usertool` wraps `[[user_tools]]` entries from `~/.bee/config.toml` as model-callable subprocess tools.
  - **Common**: `truncate.go` caps tool-result payload at the profile's `ToolOutputTokens`; `relpath.go` keeps paths repo-relative; `argparse.go` normalizes mixed-shape inputs from different providers.

  `buildToolsFiltered(cwd, writeRe)` in `cmd/bee/run_tools.go` threads a write-path regex into every mutation tool for confined runs. `Engine.Run` dispatches a turn's read-only tools concurrently and runs mutators/shell serially as barriers (see `dispatchTools` in `internal/loop/turn_tools.go`): all in-flight reads complete before a mutator runs, and nothing new starts until it returns, preserving happens-before and avoiding sandbox contention.

- **`internal/prompt/`** — assembles the per-turn system prompt: caveman rules + identity + tool manifest + skills + selected memories. Honors the active profile's `SystemPromptBudget` by truncating low-priority sections. `atexpand.go` resolves `@file` references; `context.go`/`context_warning.go` track approaching window limits.

- **`internal/commands/`** — slash-command registry for the TUI (`/login`, `/logout`, `/compact`, `/model`, etc.). Commands depend on a `Side` interface implemented by the TUI so the registry stays decoupled from Engine/TUI internals.

- **`internal/auth/`** — OAuth 2.0 PKCE flow for the ChatGPT provider. `flow.go` does the token exchange, `server.go` is the loopback callback listener, `jwt.go` decodes the OIDC `id_token` to cache `chatgpt_account_id`, `storage.go` persists tokens to `~/.bee/auth/`.

- **`internal/approval/`** — gates dangerous shell commands behind a user decision. `safety.DetectDangerous` flags a command → `Approver.Request` asks. Decisions cache for the session; `AllowAlways` persists via the caller-supplied callback (writes `config.command_allowlist`). CLI implementation for headless; TUI implements its own approver. `Static{AllowOnce}` is the auto-approve path for the `--yes`/`--yolo` flag or the persisted `yolo` toggle (`cfg.Yolo`).

- **`internal/cost/`** — process-local thread-safe tracker for per-turn token usage and dollar cost. Consumed by the TUI status bar (live session total) and the cost-monitor pane (historical breakdown). Prices come from `llm/models.go`.

- **`internal/safety/`** — defense-in-depth guards on top of the sandbox: secret redaction on tool output, path/shell-command checks that refuse obviously sensitive targets (`~/.ssh`, `.env`, etc.) even when sandbox scope would allow it. `DetectDangerous` feeds the approval gate.

- **`internal/jsonmode/`** — NDJSON event emitter for `bee run --json`. Decoupled from `llm.Usage` to avoid an import cycle.

- **`internal/skills/`** — parser (`parse.go`), in-memory registry (`registry.go`). `recipe.go` folds a legacy recipe into a prompt the model walks; `recipe_runtime.go` validates a runtime recipe (every step kinded, `next`/`on_failure` targets resolve, `fail_if` compiles, `kind: tool` may not name `bash`) and sets `Skill.Runtime`. Skills are surfaced via the `bee <name>` dispatcher in `cmd/bee/main.go` — there are no shell shims or PATH mutations. `bundled/` ships defaults (`about.md`, `calc.md`, `caveman-commit.md`, `caveman-review.md`, `check-tests.md`, `criticize.md`, `efficient-search.md`, `explore.md`, `hermes.md`, `plan.md`, `research.md`, `session.md`, `ultraplan.md`) as `embed.FS`; `WriteDefaults` is called on first run and preserves user edits.

- **`internal/recipe/`** — executor for runtime recipes. `Run(ctx, host, skill, opts)` walks the steps: `shell` runs through the host's gated `bash` tool (sandbox + approval apply, every templated value is shell-quoted on substitution), `tool` calls a registered tool with templated JSON args, `bee` hands one bounded turn to a model (optional tool allowlist) and captures its final text. Templates are plain `{{args}}`, `{{argv.N}}`, `{{<step-id>}}`; an unknown placeholder fails the step rather than expanding empty. Flow: `next` (following step | `stop` | `escalate` | step id) after success, `on_failure` (abort | `escalate` | step id) after a failure, `fail_if` regexp turns a matching output into a failure so a bee step can branch by answer. A step budget caps cycles. `Host` is the only seam: `cmd/bee/recipe_host.go` implements it over `Engine.CallTool` (`internal/loop/call_tool.go`, runs a tool outside a turn through the model-call path minus the per-turn gate) and `hive.SpawnWorker`. Exit codes from `bee <recipe>`: 0 ok, 8 escalated, 1 failed; stdout carries only the last step's output, progress and streamed bee text go to stderr.

- **`internal/knowledge/`** — per-project on-disk knowledge store. Frontmatter MD records with freeform tags + explicit priority + optional expiry. Parallel `scan.go` reads headers only (mtime-sorted, capped); `query.go` calls a side-channel LLM to extract 1–3 keyword hints and ranks entries by tag overlap + priority + recency. `age.go` produces freshness annotations; >1d-old records get a "verify before asserting" warning when injected.

- **`internal/waggle/`** — procedure memory. The miner (`recorder.go`/`miner.go`/`manager.go`) watches the loop's tool stream and crystallizes a route seen `K`+ times into a skill under `~/.bee/waggle/{proj/<hash>,user}/skills/*.md`. A read-only route becomes an exec-skill (replayable). A route with shell steps that change state is mined only when `MineConfig.Mutating` is on and every call in the window was verified by `Manager.MarkOutcome(true)` (an unattended loop reporting commit-or-reset: `bee zzz` marks commits that passed the verify gate and no-op iterations kept, reset and failed ones dropped; an ungated commit or a repair iteration leaves its span unverified); it is rendered by `promote_recipe.go` as a runtime recipe waggle with `kind: shell` steps, no `exec:` (never a model tool) and no `route:` (never replayed). The only way to run one is `bee <name>`, where each step passes the approval gate. File edits and writes are never crystallized (content, not procedure), and a shell step whose command varied across occurrences is refused. Predictive replay (`replay.go`) matches a stored prefix in Go at zero prompt cost and runs the literal remainder off the model's path, folding output into the triggering tool result; read-only, so a wrong match wastes a little read work and never mutates. `ledger.go` logs reuse and divergences; `bee waggle ls|gc` (`list.go`/`curate.go`) ranks by estimated tokens saved, prunes stale routes, demotes chronically-diverging ones, and promotes cross-project routes to user scope. `factory.go` builds the per-engine `Manager`/`Replayer`. Wired via `Engine.Waggle`/`Engine.Replay`, gated by `[waggle] enabled` (default on), active in worker/scout/queen and queen-spawned hive workers (each worker gets its own instances). Never enters the system prompt, so context cost stays O(1) in library size.

- **`internal/sandbox/`** — codex two-axis policy: `scope ∈ {read-only, workspace-write, danger-full-access}` × `approval ∈ {untrusted, on-request, on-failure, never}`. `macos.go` builds `sandbox-exec` profiles; `linux.go` builds `bwrap` invocations; `windows.go` stubs to WSL2. `Wrap(p, cmd)` is dispatch-on-`runtime.GOOS`. **Graceful degrade**: when `bwrap`/`sandbox-exec` is missing, returns the original cmd plus a warning — the sandbox is best-effort hardening, not a security boundary.

- **`internal/caveman/`** — prompt-injection compression. Rules embedded as `embed.FS` (`rules/{full,lite,ultra}.md`). `Inject(systemPrompt, level)` prepends. Default is `Full`. Caveman applies to bee's *responses*, not user input.

- **`internal/config/`** — TOML config with merge chain: `Defaults() → ~/.bee/config.toml → env (BEE_MODEL, BEE_PROVIDER, BEE_CAVEMAN, BEE_PROFILE)`. Profiles (`tiny|normal|large|auto`) tune the system-prompt budget, memory top-k + body cap, tool description chars, skill manifest chars, caveman level, iter cap, tool-output token cap, sampling temperature/top-p, read default/max lines, grep match cap, and whether `apply_patch` ships in the manifest (`SkipApplyPatch`). `auto` resolves via `ResolveAutoProfileForProvider(provider, model)` — local providers (`ollama`/`lmstudio`) always resolve to `tiny`. `local_provider.go` handles ollama/lmstudio probing; `scale.go` rescales budgets on context-window changes. `swarm.go` holds `[swarm]`: the fallback verify gate, `max_load_ratio`, `max_hops`, `max_resumes`, and the `worker`/`reviewer` model tiers.

- **`internal/session/`** — append-only JSONL rollouts under `~/.bee/sessions/<uuid>.jsonl`. Parent-pointer tree via `branch.go` (`BuildTree`, `LinearPath`) — message history is a tree, not a list. `Append` is mutex-guarded with sync-on-write.

- **`internal/hive/`** — multi-bee swarm runtime. `Pool` (fan-out, semaphore-bounded, ctx-cancellable) and `Queen` (planner decomposes a task into ≤8 sub-tasks → workers execute → planner synthesizes). The runtime concept (`hive.Worker`) is intentionally separate from the UI concept (`tui.Bee`). Two colony mechanisms keep workers aligned without talking to each other: the **pheromone card** (`pheromone.go`) is one shared card per run (goal verbatim, plan, one status row per sibling) that every worker carries in its prompt via `loop.Engine.PromptTail`, re-rendered each turn so a sibling's finish shows up next round, and mirrored to `<beeHome>/sessions/hive/<id>.pheromone.md` (the planner session for a queen run, a run id for `bee fan`, whose `Pool.Goal` heads the card and labels rows by worker name) so a second terminal can watch the hive; the **exit contract** (`report.go`) tells every worker it is disposable and that only its `## report` block (status / did / left / next) survives, so a stuck bee reports blocked with a hand-off instead of thrashing. A worker that dies anyway (iteration cap, escalate, stream failure) gets a harness-built death report distilled from its transcript (`loop.BuildDeathReport`), which reaches the synthesizer and the card instead of a bare error. Runners opt in through two optional interfaces (`Tailed`, `Reporter`); `loop.Engine` satisfies both, test stubs need neither. `Pool` and `Queen` carry a `Throttle` (`throttle.go`) that holds extra workers after they take their slot while load per core is over `[swarm] max_load_ratio`; the first worker never waits and a held one starts once its siblings finish, so load slows a hive down but never stalls it. `spawn_tier.go` routes engines by tier (`NewTiers(parent).Spawn(label, tier)`, one provider per distinct tier, built through `Engine.ProviderFor`): the TUI queen puts workers on `[swarm.worker]`, reviewers and the verifier on `[swarm.reviewer]`, the planner on the session model; `bee swarm` uses the worker tier unless `--worker-model` is given. A tier that cannot be built falls back to the parent provider with a notice.

- **`internal/agents/`** — per-agent worktree + lockfile lifecycle for `bee agents`. `spawn.go` detaches a headless engine into its own git worktree with a pinned session id; `lock.go` claims the worktree so a second spawn can't collide; `detach_unix.go`/`detach_other.go` are the platform branches; `merger.go` brings changes back: only a `done` agent is merged, under the merge lock it rebases, pins HEAD, refuses a worktree with uncommitted or untracked files (free bounce), blocks a branch that edits `.bee/verify` for a human, runs the verify gate under the agents' workspace-write sandbox (`internal/gate`, read from the main checkout so a branch cannot weaken it), re-checks that HEAD and the branch ref still equal the pinned SHA, and fast-forwards `main` to that SHA. A conflict is a free bounce, a red verify costs a hop (`bgreg.Status.Hops`, past `[swarm] max_hops` the agent is blocked for a human); either way `resume.go` re-starts the stopped agent on its worktree with the task restated plus the feedback, spending one of `[swarm] max_resumes` (a `DONE` resets the count). An `active` agent whose pid is dead is resumed the same way. Blocked agents (`MergeStateBlocked`) are never touched again by the merger. `clear.go` cleans up finished agents.

- **`internal/worktree/`** — low-level ephemeral `git worktree` checkout helper so concurrent workers each mutate files without racing on one shared tree. Used by `agents`/`hive`/`zzz`; distinct from the higher-level lifecycle in `internal/agents`.

- **`internal/remote/`** — the `bee remote-control` web relay: `server.go` serves a small control UI, `sse.go` streams turn events, `lan.go` resolves the LAN address, `qr.go` renders the connect URL as a terminal QR code.

- **`internal/ask/`** — backs the `ask_user` tool: poses a multiple-choice question and blocks until the user picks. Mirrors the approval gate — TUI surfaces an interactive picker, headless runs auto-resolve so nothing hangs.

- **`internal/goal/`** — powers the `/goal` completion-condition loop: keep running turns until a fast model judges a user-specified condition met. Pure state + bookkeeping, no `llm` import.

- **`internal/bench/`** — the `bee bench` small-model benchmark: `task.go`/`checks.go` define scored tasks, `score.go`/`metrics.go` grade runs, `ledger.go` persists results, `holdout.go` keeps a held-out split, `report.go` renders the summary.

- **`internal/bgreg/`** — per-session status sidecar for background bees. The bg engine writes one JSON file per session at `<beeHome>/sessions/bg/<id>.status.json` (temp+rename for atomic replacement); the agent-view TUI reads it. `gc.go` evicts stale entries; `inbox.go` is the cross-agent message hand-off.

- **`internal/gate/`**: the verify gate unattended work must pass before it lands (taken from sw4rm). Commands come from the repo's `.bee/verify` (one per line, `#` comments, optional `$ `), else `[swarm] verify` (an emptied file falls back too); none means no gate. `Run` executes under the caller's sandbox scope (`Opts.Scope`), because verify runs code the author wrote and an unconfined gate would let a confined bee escape; it stops at the first red command, kills a timed-out command's whole process group, and holds the machine-wide heavy lock (`<beeHome>/locks/heavy`, flock) so parallel bees never stack test suites. Used by `bee zzz` and the `bee agents` merger.

- **`internal/sysload/`**: 1-minute load average per core (sysctl on darwin, `/proc/loadavg` on linux, unknown elsewhere). `Wait(ctx, ratio, onHold, release)` holds while the box is above `[swarm] max_load_ratio` (default 2.0, 0 disables) until `release` reports true; agents release after 10 minutes since they cannot see their siblings.

- **`internal/sentinel/`** — centralized loop-control markers an unattended agent uses to signal turn outcomes. Both `bee zzz` and `bee agents` speak the same regex protocol; the status enums they write to disk stay distinct (zzz tracks RUN lifecycle, bgreg tracks AGENT-turn state).

- **`internal/zzz/`** — the overnight-loop driver. `loop.go` runs turn → verify → commit → next-objective with sentinel detection. `verify.go` gates every commit on `internal/gate`: a red gate keeps the change uncommitted for the next iteration to fix (`repair`, one hop, the failing output lands in notes.md), and past `[swarm] max_hops` the change is rolled back; `DONE` is ignored while the tree is red; `--no-gate` skips it. `git.go` does the commit; `gc.go` evicts old artifacts; `drive.go` is the supervisor.

- **`internal/update/`** — probes GitHub for new commits on `main`, applies updates by re-running `install.sh` in a subprocess. Used by the TUI background-checker. `Probe` is cheap + side-effect-free (safe on a timer); `Apply` is only invoked from an explicit user decision in the modal.

- **`internal/tui/`** — Bubbletea, inline mode: finalized messages are `tea.Println`'d into native scrollback, `View()` owns only the live region (streaming text, busy row, prompt frame, status footer). `app.go` is the root model; `app_update*.go` splits the update reducer by concern (stream, panes, pickers, session, gates); `app_pumps.go` runs the per-turn side calls (recap, mode classifier). `view.go` composes the frame: `view_live.go` is the streaming turn (reasoning, text, running calls, busy row), `view_bottom.go` is the input between two hairlines plus the picker strips, `statusline.go` is the footer (registry of items, shed in `statusDropOrder` when narrow). `stream_*.go` is the transcript: `stream_message.go` (a blank row between blocks; consecutive tool messages stack tight), `stream_blocks.go` (your turn = honey bar on a warm panel, `⬢` assistant marker with hanging indent, `✻` reasoning), `stream_toolcard.go` (`● Verb  args` call row colored by outcome, `⎿` result rows; reads collapse to a line count, bash previews 5 rows, edits and patches show one numbered diff: `stream_diff_rows.go` is the shared row model, `stream_diff_patch.go` parses unified diffs, and an edit's line numbers are read off the edit tool's own `# region` echo), `stream_spinner.go` (the busy row; pinned braille loaders via `tui.loader` in config or `BEE_LOADER`, which wins), `md_style.go` + `md_table.go` + `syntax.go` (glamour and chroma styles; each markdown table renders at its natural width, not the full terminal), `glyphs.go` (every symbol in one place). The transcript lays out down to `minRenderWidth` (20 columns) and every row it prints must fit the terminal (`stream_narrow_test.go` and `view_narrow_test.go` assert it): a row wider than the terminal is soft-wrapped by it and tears the inline redraw. Streaming text renders settled paragraphs through markdown and keeps the paragraph being typed raw (`streamMarkdown`). Tool output is **ANSI-stripped** before display (raw escapes from subprocesses like `go test` would otherwise blit over chrome) and **tabs are expanded** on every path to the terminal (a tab skips cells without erasing them, so a redrawn row keeps stale characters). `palette.go`/`picker.go` is the fzf-style `Ctrl+P` palette (provider / model / skills / slash-commands in one); `hive.go`/`workspace.go`/`session_tree.go`/`agents/` are auxiliary panes (`Ctrl+H`/`Ctrl+W`/`Ctrl+T`/`Ctrl+A`); a full pane replaces the live region while open and hugs its rows (`boxModal`, `boxRows` for aligned columns), selection is always `selectMark`. Everything that goes to scrollback goes through `scrollback.go` (`m.scroll.print`), never a bare `tea.Println`: bubbletea runs each returned Cmd on its own goroutine, so Printlns from back-to-back updates arrive swapped; the queue drains in order. A run of runes read in one chunk is fed to `Update` a rune at a time (`key_runes.go`): bubbletea names it like a key, so the word `up` would otherwise recall history. `csi_input.go` decodes CSI-u keyboard input. Slash commands route through `internal/commands` via the `Side` adapter in `side.go`.

## Conventions

- **Pure Go, no CGo.** Single static binary on darwin/linux/windows. New deps must be CGo-free.
- **≤300 lines per file.** Split if a file grows; see `wire/openai_stream.go` for an example split.
- **Internal types own the wire boundary.** Add a new provider by writing an adapter under `internal/llm/` that translates to/from `types.Message`/`ToolUse`/`ToolResult`. Do not propagate provider SDK types into other packages.
- **`Engine.Run` dispatches read-only tools concurrently, mutators serially.** A mutator/shell call is a barrier: in-flight reads drain before it runs, and nothing new starts until it returns (`dispatchTools` in `internal/loop/turn_tools.go`). A read-only tool that blocks delays only its own batch; a mutator that blocks stalls the whole turn — keep that in mind when adding one.
- **No provider name-drops in code comments.** Describe behavior, not vendor ("OpenAI-compatible chat completions wire" beats "OpenAI / DeepSeek / Groq"). Vendor names are fine in user-facing strings and config defaults.
- **Pre-set `lipgloss` dark background + glamour `WithStandardStyle("dark")`** before Bubbletea grabs stdin. See `cmd/bee/tui.go` for why (Ghostty/iTerm reply to OSC 11 queries with bytes that leak into the textinput in altscreen mode).
- **TUI styles live in `internal/tui/style.go`.** Palette is a layered neutral scale (Oyster → Squid → Smoke → Ash → Butter foregrounds; Pepper → BBQ → Charcoal → Iron backgrounds) with a single honey accent (`#FFB000`) and a few quiet semantic hues (diff green/red, link blue, inline-code teal). Borrowed from charmbracelet/charmtone but inlined to avoid the dep. Chrome stays dim; honey marks what is live or yours: the prompt chevron, your turns, the spinner, the selected row. `Model` holds `Styles` by pointer: `Model` is copied by value on every `Update`/`View` call, and the styles struct is large (see `BenchmarkView_*`).
- **Tests are first-class.** Every package has `_test.go` siblings; `go test ./...` must stay green. Use `BEE_TEST_PROVIDER=stub` or `scripted` for offline e2e — never hit a real API from a unit test.

## Adding things

- **A new tool**: create `internal/tools/<name>/`, implement `tools.Tool` (`Spec` + `Run`), export `New() tools.Tool` (and `NewWithFilter` if it mutates files — `buildToolsFiltered` expects it). Wire it into both `buildToolsWithApprover` and `buildToolsFilteredWithApprover` in `cmd/bee/run_tools.go` so headless + TUI + fan + swarm + agents + write-confined runs all pick it up. If it should be read-only safe (available to scout and read-only worker turns), also list it in `internal/loop/role.go` (`readOnlyTools`, or `scoutExtraTools` for scout-only web tools).

- **A new slash command**: implement `commands.Command` and register it in `internal/commands/builtins.go`. If it needs Engine/TUI state, add the method to the `Side` interface and implement it in `internal/tui/side.go`.

- **A new provider**: add `internal/llm/<name>.go` returning the `Provider` interface plus a `wire/<name>.go` translator. Wire it into the `WireAPI` switch in `cmd/bee/run_provider.go`. If it's OpenAI-compatible, just add a `[providers.<name>]` block to `internal/config/defaults.go` — `openai_compat.go` handles it without code. For local/tiny models, consider setting `ToolFormat="xml"` in the matching profile so `textmode` wraps the provider.

- **A bundled skill**: add a `.md` file under `internal/skills/bundled/` with the right frontmatter (`type`, `description`, etc.). It auto-installs on first run and is invokable as `bee <skill-name>`. The skill is also exposed as a model-callable tool unless the frontmatter opts out. For a runtime recipe give every step a `kind` plus `run` (shell), `tool`+`args` (tool) or `prompt`+optional `tools` (bee); see `check-tests.md`. Do not quote a placeholder inside `run` yourself, substitution already single-quotes it.

- **A new TUI pane**: define a sentinel `openXMsg` in `app.go`, bind a key in `keymap.go`, write the component in `internal/tui/<pane>.go` (or `internal/tui/<pane>/` if it grows). Use `lipglossWidth`/`truncateVisible` from `util.go` for ANSI-safe sizing. Route any slash command for it through `internal/commands` + `side.go`, not directly into the model.

- **A new profile**: add an entry to `internal/config/defaults.go`'s `Profiles` map and a branch in `ResolveAutoProfileForProvider` if it should be selectable via `profile="auto"`. Tune `SystemPromptBudget`, `MemoryTopK`, `ToolDescChars`, `SkillManifestChars`, `ToolOutputTokens`, `ReadDefaultLines`/`ReadMaxLines`, `GrepMaxMatches`, `Caveman`, and the sampling params together.

## Environment variables

| Var | Purpose |
|---|---|
| `BEE_HOME` | Override `~/.bee` (hermetic tests). |
| `BEE_PROVIDER` | Override config `default_provider`. |
| `BEE_MODEL` | Override config `default_model`. |
| `BEE_PROFILE` | Override config `profile` (`tiny`/`normal`/`large`/`auto`). |
| `BEE_CAVEMAN` | Override caveman level. |
| `BEE_STREAM_STALL_SECONDS` | Override the streaming idle-stall window (default 600s). `<=0` disables the watchdog. |
| `BEE_TEST_PROVIDER` | `stub` for canned replies, `scripted` for fixture-driven runs. |
| `BEE_TEST_SCRIPT` | Path to scripted fixture when `BEE_TEST_PROVIDER=scripted`. |
| `OPENROUTER_API_KEY` / `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GEMINI_API_KEY` / ... | Provider keys; resolved from `EnvKey` on the active provider block. |
