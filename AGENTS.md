# AGENTS.md

Guidance for agents (and humans) working on this repository. Read this **before** any non-trivial change.

## Project

`bee` is a pure-Go single-binary coding agent. Three intentional wedges over other CLI coding agents:

1. **Skills are `bee <name>` subcommands.** `~/.bee/skills/<name>.md` is invokable as `bee <name> [args...]` — one binary, one PATH entry, no shell shims sprayed onto `$PATH`. Unknown `arg[1]` falls through to skill-registry lookup via `dispatchSkill`.
2. **Skills are agent endpoints.** Four kinds: `prompt` | `exec` | `mcp` | `http`. The same skill is surfaced both as a `bee <name>` subcommand AND a model-callable tool the agent can invoke mid-task.
3. **Tiny-context friendly.** System-prompt budget is configurable per-profile (`tiny|normal|large|auto`); memory injection is lazy top-k; tool descriptions and skill manifest are token-budgeted. Designed to run against a 4k-context local Ollama as well as frontier APIs.

Other load-bearing choices: `apply_patch` collapses write/edit/multi-edit on capable models (tiny profile swaps it out for `write`+`edit`+`hashline_edit`); codex-style two-axis sandbox; frontmatter knowledge store with lazy top-K selection; textmode wrapper that emits XML-style tool calls for local models that ignore `tool_calls`.

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
| `zzz` | Overnight autonomous-commit loop. Same engine, sentinel-driven stop. |
| `doctor` | Read-only preflight: provider keys, sandbox tools, ollama probe, models cache. |
| `version` / `-v` / `--version` | Build version. |
| `help` / `-h` / `--help` | Usage. |
| *anything else* | `dispatchSkill(arg[1], rest...)` → headless run with `--skill <name>`. |

`stub_provider.go` is gated by `BEE_TEST_PROVIDER=stub` (or `scripted` for fixture-driven runs) so the binary works offline in tests.

## Architecture

Clean **types → provider → tools → loop → ui** stack. Internal packages talk via the interfaces in `internal/types`, `internal/llm/provider.go`, `internal/tools/registry.go`. Implementations stay swappable.

- **`cmd/bee/`** — entry + subcommand wiring. `main.go` is a stdlib switch (see table above). `run.go` is the headless engine path; `run_tools.go` builds the tool registry (with optional `writeRe` path filter for confined runs and an `approval.Approver` for dangerous-command gating); `run_provider.go` resolves provider/model from config + env. `tui.go` wires the same Engine into the Bubbletea app. `fan.go`/`swarm.go`/`hyperplan.go`/`hive.go` build N engines for multi-bee work. `bg.go` daemonizes; `agents.go` opens the parallel-agents pane; `zzz.go` runs the overnight loop. `doctor.go` is the preflight. `stub_provider.go` is gated by `BEE_TEST_PROVIDER`.

- **`internal/loop/`** — the agent turn loop. `Engine.Run(ctx, userMsg)` selects knowledge entries → assembles system prompt → streams provider events → dispatches tool calls serially → folds results → recurses. Hard `MaxIterations` cap (config + profile override). `Mode ∈ {plan, edit, auto}`: plan = read-only tool surface (`profile_tools.go`), edit = full surface, auto = per-turn classifier in `mode.go`. `compact.go` summarizes mid-history when context fills; `recap.go` produces post-turn end-of-task recaps when enabled. `done_signal.go` + sentinel markers let unattended loops detect "I'm done". `sandbox_wrap.go` wraps shell calls with the active sandbox policy. `KnowledgeStore` is an interface so the loop never imports `internal/knowledge` directly.

- **`internal/llm/`** — `Provider` interface + adapters. Built-ins:
  - `openai_compat.go` — OpenRouter / OpenAI / DeepSeek / Groq / Ollama / LM Studio via `base_url + wire_api=chat`. Streaming in `openai_compat_stream.go`; stall-watchdog in `openai_compat_stall_test.go`.
  - `claude.go` — native Anthropic Messages API (`wire_api=anthropic-messages`), streaming in `claude_stream.go`, thinking-block aware.
  - `chatgpt.go` — OAuth-backed ChatGPT account via `internal/auth` (`wire_api=responses`); request/stream split across `chatgpt_request.go`/`chatgpt_stream.go`.
  - `gemini.go` — native Google Gemini (`wire_api=gemini`).
  - `textmode.go` — wraps another `Provider`, injects an XML-style tool advert into the system prompt and parses `<tool>{...}</tool>` envelopes out of the assistant content stream. Opt-in per profile via `ToolFormat="xml"` for tiny/local models that ignore `tool_calls`. Parser in `textmode_parse.go`.
  - `thinking_hybrid.go` — handles providers that emit reasoning in a side channel vs. inline `<thinking>` tags.
  - `models.go` + `models_cache.go` + `models_hardcoded.go` — model registry with on-disk cache; pricing fuels `internal/cost`.
  - `wire/` — translates internal `types.Message`/`ToolUse`/`ToolResult` to/from each provider's wire format: `openai.go`/`openai_stream.go`, `anthropic_messages.go`/`anthropic_messages_stream.go`, `responses.go`/`responses_stream.go`, `gemini.go`. **Internal message types are agent-owned — never leak provider SDK types upward.**
  - `mockprov/` — fixture-driven `Provider` for scripted e2e tests.

- **`internal/tools/`** — current surface (Spec name in `tools/<dir>/<dir>.go`):
  - **Read-side**: `read`, `search` (regex grep, code in `internal/tools/grep/`), `glob` (filename match, code in `internal/tools/find/`), `ls`.
  - **Write-side**: `apply_patch` (unified-diff multi-edit; tiny profile skips it), `write`, `edit` (search-and-replace, code in `internal/tools/edit_diff/`), `hashline_edit` (line-number based, robust on tiny models).
  - **Shell**: `bash` (code in `internal/tools/shell/`, wrapped by sandbox policy + `internal/approval` for dangerous-command gating).
  - **Knowledge**: `knowledge_search`, `knowledge_write` — frontend to `internal/knowledge`. Disabled when `[memory] enabled=false`.
  - **Meta**: `tool_lookup` — model-callable "what tools do I have, and how do I use them?" Reads back from the registry so it always sees the live filtered surface, including user-defined tools.
  - **Config-defined**: `usertool` wraps `[[user_tools]]` entries from `~/.bee/config.toml` as model-callable subprocess tools.
  - **Common**: `truncate.go` caps tool-result payload at the profile's `ToolOutputTokens`; `relpath.go` keeps paths repo-relative; `argparse.go` normalizes mixed-shape inputs from different providers.

  `buildToolsFiltered(cwd, writeRe)` in `cmd/bee/run_tools.go` threads a write-path regex into every mutation tool for confined runs. `Engine.Run` dispatches tools serially — there is no tool parallelism in v0.1.

- **`internal/prompt/`** — assembles the per-turn system prompt: caveman rules + identity + tool manifest + skills + selected memories. Honors the active profile's `SystemPromptBudget` by truncating low-priority sections. `atexpand.go` resolves `@file` references; `context.go`/`context_warning.go` track approaching window limits.

- **`internal/commands/`** — slash-command registry for the TUI (`/login`, `/logout`, `/compact`, `/model`, etc.). Commands depend on a `Side` interface implemented by the TUI so the registry stays decoupled from Engine/TUI internals.

- **`internal/auth/`** — OAuth 2.0 PKCE flow for the ChatGPT provider. `flow.go` does the token exchange, `server.go` is the loopback callback listener, `jwt.go` decodes the OIDC `id_token` to cache `chatgpt_account_id`, `storage.go` persists tokens to `~/.bee/auth/`.

- **`internal/approval/`** — gates dangerous shell commands behind a user decision. `safety.DetectDangerous` flags a command → `Approver.Request` asks. Decisions cache for the session; `AllowAlways` persists via the caller-supplied callback (writes `config.command_allowlist`). CLI implementation for headless; TUI implements its own approver. `Static{AllowOnce}` is the auto-yes shortcut for `--auto-yes`.

- **`internal/cost/`** — process-local thread-safe tracker for per-turn token usage and dollar cost. Consumed by the TUI status bar (live session total) and the cost-monitor pane (historical breakdown). Prices come from `llm/models.go`.

- **`internal/safety/`** — defense-in-depth guards on top of the sandbox: secret redaction on tool output, path/shell-command checks that refuse obviously sensitive targets (`~/.ssh`, `.env`, etc.) even when sandbox scope would allow it. `DetectDangerous` feeds the approval gate.

- **`internal/jsonmode/`** — NDJSON event emitter for `bee run --json`. Decoupled from `llm.Usage` to avoid an import cycle.

- **`internal/skills/`** — parser (`parse.go`), in-memory registry (`registry.go`). Skills are surfaced via the `bee <name>` dispatcher in `cmd/bee/main.go` — there are no shell shims or PATH mutations. `bundled/` ships defaults (`calc.md`, `caveman-commit.md`, `caveman-review.md`, `criticize.md`, `efficient-search.md`, `hermes.md`, `ultraplan.md`, ...) as `embed.FS`; `WriteDefaults` is called on first run and preserves user edits.

- **`internal/knowledge/`** — per-project on-disk knowledge store. Frontmatter MD records with freeform tags + explicit priority + optional expiry. Parallel `scan.go` reads headers only (mtime-sorted, capped); `query.go` calls a side-channel LLM to extract 1–3 keyword hints and ranks entries by tag overlap + priority + recency. `age.go` produces freshness annotations; >1d-old records get a "verify before asserting" warning when injected.

- **`internal/sandbox/`** — codex two-axis policy: `scope ∈ {read-only, workspace-write, danger-full-access}` × `approval ∈ {untrusted, on-request, on-failure, never}`. `macos.go` builds `sandbox-exec` profiles; `linux.go` builds `bwrap` invocations; `windows.go` stubs to WSL2. `Wrap(p, cmd)` is dispatch-on-`runtime.GOOS`. **Graceful degrade**: when `bwrap`/`sandbox-exec` is missing, returns the original cmd plus a warning — the sandbox is best-effort hardening, not a security boundary.

- **`internal/caveman/`** — prompt-injection compression. Rules embedded as `embed.FS` (`rules/{full,lite,ultra}.md`). `Inject(systemPrompt, level)` prepends. Default is `Full`. Caveman applies to bee's *responses*, not user input.

- **`internal/config/`** — TOML config with merge chain: `Defaults() → ~/.bee/config.toml → env (BEE_MODEL, BEE_PROVIDER, BEE_CAVEMAN, BEE_PROFILE)`. Profiles (`tiny|normal|large|auto`) tune the system-prompt budget, memory top-k + body cap, tool description chars, skill manifest chars, caveman level, iter cap, tool-output token cap, sampling temperature/top-p, read default/max lines, grep match cap, and whether `apply_patch` ships in the manifest (`SkipApplyPatch`). `auto` resolves via `ResolveAutoProfileForProvider(provider, model)` — local providers (`ollama`/`lmstudio`) always resolve to `tiny`. `local_provider.go` handles ollama/lmstudio probing; `scale.go` rescales budgets on context-window changes.

- **`internal/session/`** — append-only JSONL rollouts under `~/.bee/sessions/<uuid>.jsonl`. Parent-pointer tree via `branch.go` (`BuildTree`, `LinearPath`) — message history is a tree, not a list. `Append` is mutex-guarded with sync-on-write.

- **`internal/hive/`** — multi-bee swarm runtime. `Pool` (fan-out, semaphore-bounded, ctx-cancellable) and `Queen` (planner decomposes a task into ≤8 sub-tasks → workers execute → planner synthesizes). The runtime concept (`hive.Worker`) is intentionally separate from the UI concept (`tui.Bee`).

- **`internal/agents/`** — per-agent worktree + lockfile lifecycle for `bee agents`. `spawn.go` detaches a headless engine into its own git worktree with a pinned session id; `lock.go` claims the worktree so a second spawn can't collide; `detach_unix.go`/`detach_other.go` are the platform branches; `merger.go` handles bringing changes back; `clear.go` cleans up finished agents.

- **`internal/bgreg/`** — per-session status sidecar for background bees. The bg engine writes one JSON file per session at `<beeHome>/sessions/bg/<id>.status.json` (temp+rename for atomic replacement); the agent-view TUI reads it. `gc.go` evicts stale entries; `inbox.go` is the cross-agent message hand-off.

- **`internal/sentinel/`** — centralized loop-control markers an unattended agent uses to signal turn outcomes. Both `bee zzz` and `bee agents` speak the same regex protocol; the status enums they write to disk stay distinct (zzz tracks RUN lifecycle, bgreg tracks AGENT-turn state).

- **`internal/zzz/`** — the overnight-loop driver. `loop.go` runs turn → commit → next-objective with sentinel detection; `git.go` does the commit; `gc.go` evicts old artifacts; `drive.go` is the supervisor.

- **`internal/update/`** — probes GitHub for new commits on `main`, applies updates by re-running `install.sh` in a subprocess. Used by the TUI background-checker. `Probe` is cheap + side-effect-free (safe on a timer); `Apply` is only invoked from an explicit user decision in the modal.

- **`internal/tui/`** — Bubbletea. `app.go` is the root model; `app_update*.go` splits the update reducer by concern (stream, panes, pickers, session, gates); `app_pumps.go` runs the per-turn side calls (recap, mode classifier). `view.go` renders top bar + scrollback + bottom bar; `stream.go` does role glyphs (`▸` for user; tool turns intentionally have none), markdown via glamour, and **ANSI-strips tool output** before display (raw escapes from subprocesses like `go test` would otherwise blit over chrome in altscreen). `palette.go`/`picker.go` is the fzf-style `Ctrl+P` palette (provider / model / skills / slash-commands in one); `hive.go`/`workspace.go`/`session_tree.go`/`agents/` are auxiliary panes (`Ctrl+H`/`Ctrl+W`/`Ctrl+T`/`Ctrl+A`). `csi_input.go` decodes CSI-u keyboard input. Slash commands route through `internal/commands` via the `Side` adapter in `side.go`.

## Conventions

- **Pure Go, no CGo.** Single static binary on darwin/linux/windows. New deps must be CGo-free.
- **≤300 lines per file.** Split if a file grows; see `wire/openai_stream.go` for an example split.
- **Internal types own the wire boundary.** Add a new provider by writing an adapter under `internal/llm/` that translates to/from `types.Message`/`ToolUse`/`ToolResult`. Do not propagate provider SDK types into other packages.
- **`Engine.Run` dispatches tools serially.** No tool parallelism in v0.1 — keep this in mind when adding tools that block.
- **No provider name-drops in code comments.** Describe behavior, not vendor ("OpenAI-compatible chat completions wire" beats "OpenAI / DeepSeek / Groq"). Vendor names are fine in user-facing strings and config defaults.
- **Pre-set `lipgloss` dark background + glamour `WithStandardStyle("dark")`** before Bubbletea grabs stdin. See `cmd/bee/tui.go` for why (Ghostty/iTerm reply to OSC 11 queries with bytes that leak into the textinput in altscreen mode).
- **TUI styles live in `internal/tui/style.go`.** Palette is a layered neutral scale (Oyster → Squid → Smoke → Ash → Butter foregrounds; Pepper → BBQ → Charcoal → Iron backgrounds) with a single honey accent (`#FFB000`). Borrowed from charmbracelet/charmtone but inlined to avoid the dep. Chrome stays dim; the bee glyph carries the accent.
- **Tests are first-class.** Every package has `_test.go` siblings; `go test ./...` must stay green. Use `BEE_TEST_PROVIDER=stub` or `scripted` for offline e2e — never hit a real API from a unit test.

## Adding things

- **A new tool**: create `internal/tools/<name>/`, implement `tools.Tool` (`Spec` + `Run`), export `New() tools.Tool` (and `NewWithFilter` if it mutates files — `buildToolsFiltered` expects it). Wire it into both `buildToolsWithApprover` and `buildToolsFilteredWithApprover` in `cmd/bee/run_tools.go` so headless + TUI + fan + swarm + agents + write-confined runs all pick it up. If it should be plan-mode safe, also list it in `internal/loop/profile_tools.go`.

- **A new slash command**: implement `commands.Command` and register it in `internal/commands/builtins.go`. If it needs Engine/TUI state, add the method to the `Side` interface and implement it in `internal/tui/side.go`.

- **A new provider**: add `internal/llm/<name>.go` returning the `Provider` interface plus a `wire/<name>.go` translator. Wire it into the `WireAPI` switch in `cmd/bee/run_provider.go`. If it's OpenAI-compatible, just add a `[providers.<name>]` block to `internal/config/defaults.go` — `openai_compat.go` handles it without code. For local/tiny models, consider setting `ToolFormat="xml"` in the matching profile so `textmode` wraps the provider.

- **A bundled skill**: add a `.md` file under `internal/skills/bundled/` with the right frontmatter (`type`, `description`, etc.). It auto-installs on first run and is invokable as `bee <skill-name>`. The skill is also exposed as a model-callable tool unless the frontmatter opts out.

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
| `BEE_TEST_PROVIDER` | `stub` for canned replies, `scripted` for fixture-driven runs. |
| `BEE_TEST_SCRIPT` | Path to scripted fixture when `BEE_TEST_PROVIDER=scripted`. |
| `OPENROUTER_API_KEY` / `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GEMINI_API_KEY` / ... | Provider keys; resolved from `EnvKey` on the active provider block. |
