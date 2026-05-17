# AGENTS.md

This file provides guidance when working with code in this repository.

## Project

`bee` is a pure-Go single-binary coding agent. Three intentional wedges over other CLI coding agents:

1. **Skills are `bee <name>` subcommands.** `~/.bee/skills/<name>.md` is invokable as `bee <name> [args...]` — one binary, one PATH entry, no shell shims sprayed onto `$PATH`. Unknown arg[1] falls through to skill registry lookup.
2. **Skills are agent endpoints.** Four kinds: `prompt` | `exec` | `mcp` | `http`. The same skill is surfaced both as a `bee <name>` subcommand AND a model-callable tool the agent can invoke mid-task.
3. **Tiny-context friendly.** System-prompt budget is configurable per-profile (`tiny|normal|large`); memory injection is lazy top-k; tool descriptions and skill manifest are token-budgeted. Designed to run against a 4k-context local Ollama as well as deepseek-v4-flash.

The architecture leans on a few load-bearing choices: read/write/edit collapsed into `apply_patch`, codex-style two-axis sandbox, frontmatter knowledge store with lazy top-K selection. Read the package docs in `internal/` before any non-trivial change.

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
```

Real OpenRouter smoke (requires `OPENROUTER_API_KEY`):

```sh
./bee run "say hi in three words"
```

First-run is implicit: `bee run` / `bee` / `bee <skill>` all call `ensureFirstRun` which creates `~/.bee/skills` and drops the bundled skills the first time it sees an empty dir.

Override `$HOME` via `BEE_HOME=/tmp/iso` for hermetic install tests.

## Architecture

The agent has a clean **types → provider → tools → loop → ui** stack. Internal packages talk via the interfaces in `internal/types`, `internal/llm/provider.go`, `internal/tools/registry.go`. Implementations stay swappable.

- **`cmd/bee/`** — entry. Subcommand dispatch in `main.go` is a tiny stdlib switch: `run | back | fan | swarm | hyperplan | hive | bg | doctor | version | help`. Unknown arg[1] falls through to `dispatchSkill` which translates `bee <skill> ...` into a headless run with `--skill <name>`. `run.go` is the headless engine path; `tui.go` reuses the same Engine wiring for interactive mode; `fan.go`/`swarm.go`/`hyperplan.go` build N engines for the hive (hyperplan = 5 critics + synthesizer queen). `bg.go` re-execs in headless mode with a pinned session id and detaches via Setsid. `doctor.go` is a read-only preflight. `stub_provider.go` is gated by `BEE_TEST_PROVIDER=stub` for offline runs.

- **`internal/loop/`** — the agent turn loop. `Engine.Run(ctx, userMsg)` selects knowledge entries → assembles system prompt → streams provider events → dispatches tool calls serially → folds results → recurses. Hard 20-iteration cap. `KnowledgeStore` is an interface so the loop doesn't depend on the concrete `internal/knowledge` package internals.

- **`internal/llm/`** — `Provider` interface + `openai_compat.go` (covers OpenRouter/OpenAI/DeepSeek/Groq/Ollama/LM Studio via `base_url + wire_api`) + `chatgpt.go` (OAuth-backed ChatGPT account via `internal/auth`) + stub `anthropic.go`. `wire/` translates internal `types.Message`/`ToolUse`/`ToolResult` to/from each provider's wire format — both Chat Completions (`openai_stream.go`) and Responses API (`responses_stream.go`). **Internal message types are agent-owned** — never leak provider SDK types upward.

- **`internal/tools/`** — current surface: `shell`, `read`, `apply_patch`, `grep`, `find`, `ls`, `write`, `edit_diff`, `hashline_edit` (+ `knowledge_search` for skill use). The original "three tools" design (`apply_patch` collapses write/edit/multi-edit) is preserved, but read-side discovery (`grep`/`find`/`ls`) and small targeted mutations (`edit_diff`/`hashline_edit`) were added back for models that fumble unified diffs. `buildToolsFiltered` in `cmd/bee/run.go` threads a write-path regex into every mutation tool for confined runs. The shell tool is wrapped by sandbox policy in the loop (via `internal/loop/sandbox_wrap.go`).

- **`internal/prompt/`** — assembles the per-turn system prompt: caveman rules + identity + tool manifest + skills + selected memories. Honors the active profile's `SystemPromptBudget` by truncating low-priority sections. `atexpand.go` resolves `@file` references; `context.go`/`context_warning.go` track approaching window limits.

- **`internal/commands/`** — slash-command registry for the TUI (`/login`, `/logout`, `/compact`, `/model`, etc.). Commands depend on a `Side` interface implemented by the TUI so the registry stays decoupled from Engine/TUI internals.

- **`internal/auth/`** — OAuth 2.0 PKCE flow for the ChatGPT provider. `flow.go` does the token exchange, `server.go` is the loopback callback listener, `jwt.go` decodes the OIDC `id_token` to cache `chatgpt_account_id`, `storage.go` persists tokens to `~/.bee/auth/`.

- **`internal/cost/`** — process-local thread-safe tracker for per-turn token usage and dollar cost. Consumed by the TUI status bar (live session total) and the cost-monitor pane (historical breakdown). Prices come from `llm/models.go`.

- **`internal/safety/`** — defense-in-depth guards on top of the sandbox: secret redaction on tool output, path/shell-command checks that refuse obviously sensitive targets (`~/.ssh`, `.env`, etc.) even when sandbox scope would allow it.

- **`internal/jsonmode/`** — NDJSON event emitter for `bee run --json`. Decoupled from `llm.Usage` to avoid an import cycle.

- **`internal/skills/`** — parser (`parse.go`), in-memory registry (`registry.go`). Skills are surfaced via the `bee <name>` dispatcher in `cmd/bee/main.go` — there are no shell shims or PATH mutations. `bundled/` ships default skills as `embed.FS`; `WriteDefaults` is called on first run and preserves user edits.

- **`internal/knowledge/`** — per-project on-disk knowledge store. Frontmatter MD records with freeform tags + explicit priority + optional expiry. Parallel `scan.go` reads headers only (mtime-sorted, capped); `query.go` calls a side-channel LLM to extract 1–3 keyword hints and ranks entries by tag overlap + priority + recency. `age.go` produces freshness annotations; >1d-old records get a "verify before asserting" warning when injected.

- **`internal/sandbox/`** — codex two-axis policy: `scope ∈ {read-only, workspace-write, danger-full-access}` × `approval ∈ {untrusted, on-request, on-failure, never}`. `macos.go` builds `sandbox-exec` profiles; `linux.go` builds `bwrap` invocations; `windows.go` stubs to WSL2. `Wrap(p, cmd)` is dispatch-on-`runtime.GOOS`. **Graceful degrade**: when `bwrap`/`sandbox-exec` is missing, returns the original cmd plus a warning — the sandbox is best-effort hardening, not a security boundary.

- **`internal/caveman/`** — prompt-injection compression. Rules embedded as `embed.FS` (`rules/{full,lite,ultra}.md`). `Inject(systemPrompt, level)` prepends. Default is `Full`. Caveman applies to bee's *responses*, not user input.

- **`internal/config/`** — TOML config with merge chain: `Defaults() → ~/.bee/config.toml → env (BEE_MODEL, BEE_PROVIDER, BEE_CAVEMAN, BEE_PROFILE)`. Profiles (`tiny|normal|large`) tune `SystemPromptBudget`, `MemoryTopK`, `ToolDescChars`, `SkillManifestChars`, and the caveman level together — pick the profile to match the model size.

- **`internal/session/`** — append-only JSONL rollouts under `~/.bee/sessions/<uuid>.jsonl`. Parent-pointer tree via `branch.go` (`BuildTree`, `LinearPath`) — message history is a tree, not a list. `Append` is mutex-guarded with sync-on-write.

- **`internal/hive/`** — multi-bee swarm. `Pool` (fan-out, semaphore-bounded, ctx-cancellable) and `Queen` (planner decomposes a task into ≤8 sub-tasks → workers execute → planner synthesizes). The runtime concept (`hive.Worker`) is intentionally separate from the UI concept (`tui.Bee`).

- **`internal/tui/`** — Bubbletea. `app.go` is the root model; `view.go` renders top bar + scrollback + bottom bar; `stream.go` does role glyphs (`▸` for user; tool turns intentionally have none), markdown via glamour, and **ANSI-strips tool output** before display (raw escapes from subprocesses like `go test` would otherwise blit over chrome in altscreen). `palette.go`/`picker.go` is the fzf-style `Ctrl+P` palette (provider/model/skills/slash-commands in one); `hive.go`/`workspace.go`/`session_tree.go` are auxiliary panes (`Ctrl+H`/`Ctrl+W`/`Ctrl+T`). `csi_input.go` decodes CSI-u keyboard input. Slash commands route through `internal/commands` via the `Side` adapter in `side.go`.

## Conventions

- **Pure Go, no CGo.** Single static binary on darwin/linux/windows. New deps must be CGo-free.
- **≤300 lines per file.** Split if a file grows; see `wire/openai_stream.go` for an example split.
- **Internal types own the wire boundary.** Add a new provider by writing an adapter under `internal/llm/` that translates to/from `types.Message`/`ToolUse`/`ToolResult`. Do not propagate provider SDK types into other packages.
- **Engine.Run dispatches tools serially.** No tool parallelism in v0.1 — keep this in mind when adding tools that block.
- **Pre-set lipgloss dark background + glamour `WithStandardStyle("dark")`** before bubbletea grabs stdin. See `cmd/bee/tui.go` for why (Ghostty/iTerm reply to OSC 11 queries with bytes that leak into the textinput in altscreen mode).
- **TUI styles live in `internal/tui/style.go`.** Palette is a layered neutral scale (Oyster → Squid → Smoke → Ash → Butter foregrounds; Pepper → BBQ → Charcoal → Iron backgrounds) with a single honey accent (`#FFB000`). Borrowed from charmbracelet/charmtone but inlined to avoid the dep. Chrome stays dim; the bee glyph carries the accent.

## Adding things

- **A new tool**: create `internal/tools/<name>/`, implement `tools.Tool` (Spec + Run), export `New() tools.Tool` (and `NewWithFilter` if it mutates files — `buildToolsFiltered` expects it). Wire it into both `buildTools(cwd)` and `buildToolsFiltered(cwd, writeRe)` in `cmd/bee/run.go` so headless + TUI + fan + swarm + write-confined runs all pick it up.

- **A new slash command**: implement `commands.Command` and register it in `internal/commands/builtins.go`. If it needs Engine/TUI state, add the method to the `Side` interface and implement it in `internal/tui/side.go`.
- **A new provider**: add `internal/llm/<name>.go` returning the `Provider` interface. If it's OpenAI-compatible, just add a `[providers.<name>]` block to `internal/config/defaults.go` — `openai_compat.go` handles it without code.
- **A bundled skill**: add a `.md` file under `internal/skills/bundled/` with the right frontmatter (`type`, `description`, etc.). It auto-installs on first run and is invokable as `bee <skill-name>`.
- **A new TUI pane**: define a sentinel `openXMsg` in `app.go`, bind a key in `keymap.go`, write the component in `internal/tui/<pane>.go`. Use `lipglossWidth`/`truncateVisible` from `util.go` for ANSI-safe sizing.
