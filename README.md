# 🐝 bee

[![CI](https://github.com/elhenro/bee/actions/workflows/ci.yml/badge.svg)](https://github.com/elhenro/bee/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/elhenro/bee.svg)](https://pkg.go.dev/github.com/elhenro/bee)
[![Go Report](https://goreportcard.com/badge/github.com/elhenro/bee)](https://goreportcard.com/report/github.com/elhenro/bee)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Release](https://img.shields.io/github/v/release/elhenro/bee)](https://github.com/elhenro/bee/releases)

![bee](./bee.png)

bee coding agent harness. Pure Go, single static binary, requires Go 1.26+ to build.

```sh
# install (curl | sh)
curl -fsSL https://raw.githubusercontent.com/elhenro/bee/main/install.sh | sh

# or via go install
go install github.com/elhenro/bee/cmd/bee@latest

# or build to your local bin
go build -o ~/.local/bin/bee ./cmd/bee

# use
export OPENROUTER_API_KEY=<your-key>
bee
```

## Why?

Three wedges incumbents miss:

1. **Tiny-context friendly, tiny footprint.** Caveman-compressed system prompt, three tools, top-k memory. Same harness scales from a 4k-context local Ollama up through small fine-tunes to million-token frontier models. Native [omlx](https://github.com/jundot/omlx) (Apple Silicon MLX server) and OpenRouter support out of the box. Shrinks itself when context gets tight.
2. **Skills are `bee <name>` subcommands.** Write a markdown file, get a command. No shell shims. No REPL incantations. `bee criticize plan.md` just works, from any directory, in any shell.
3. **Skills are agent endpoints.** A prompt, an external command, an MCP server, or an HTTP endpoint — all four are equally callable tools the model can invoke mid-task. Plug a personal-life agent in as a sub-agent (bundled `hermes.md` is a template — edit the `exec:` line). No IPC dance.

## Quick demos

```sh
$ bee criticize plan.md             # one binary, every skill a subcommand
$ bee run "lint cmd/"               # headless, pipeable
$ bee swarm "migrate auth to jwt"   # queen + workers
$ bee fan "audit internal/ for cleanup"  # parallel fan-out
```

`~/.bee/skills/*.md` is your library. Add one, it shows up. First run seeds defaults. Edit one, it lives.

## Config

`~/.bee/config.toml`, sane defaults, set an API key, change models.

## Local models

bee runs against any OpenAI-compatible local server. Confirmed working:

- [omlx](https://github.com/jundot/omlx) (Apple Silicon MLX server, `localhost:8000/v1`) with MLX-quantized coder models — strong tool-calling, low RAM footprint.
- **Ollama** (`localhost:11434/v1`) with `llama3.1:8b`, `qwen2.5-coder:7b`.
- **LM Studio** (`localhost:1234/v1`).

For sub-8k-context models, switch to the tiny profile. `--profile` is not a CLI flag — set it via env or `~/.bee/config.toml`:

    BEE_PROFILE=tiny bee run --provider omlx --model Qwen3.6-35B-A3B-4bit -- "..."

    # or persist in ~/.bee/config.toml
    profile = "tiny"
    default_provider = "omlx"
    default_model = "Qwen3.6-35B-A3B-4bit"

## Caveman mode

Token-compression rules injected into the system prompt. On by default. `caveman = "auto"` resolves per profile: `full` on `tiny` and `normal`, `lite` on `large`.

Force a level regardless of profile:

    bee --caveman full                        # global, any subcommand
    bee run --caveman full -- "..."           # one-off
    # or set caveman = "full" in ~/.bee/config.toml

Disable:

    bee --caveman off
    # or set caveman = "off" in ~/.bee/config.toml

Explicit value beats profile.

## My setup / how I run this

**Mac M3 Max (64 GB) -> omlx with an MLX-quantized coder model.**
Runs fast, handles small tasks reliably, doesn't choke on context. Good enough for day-to-day. Local, private, free once the hardware is paid for.

## Platform support

- **macOS / Linux** — first-class. Static binaries published for `darwin/{amd64,arm64}` and `linux/{amd64,arm64}`.
- **Windows** — best-effort. The native build runs; the sandbox layer is a stub that re-dispatches through WSL2. Run under WSL2 for production use.

## ChatGPT-account provider (opt-in, use at own risk)

The `chatgpt` provider lets you drive bee with a ChatGPT Plus/Pro/Team subscription via the `chatgpt.com` Codex backend instead of paying per-token API billing. **This reuses a public client_id that targets a first-party OpenAI surface. OpenAI's terms restrict that surface to their own clients — usage may be rate-limited per plan tier and the path can be revoked at any time.** Treat this provider as experimental. Use `OPENROUTER_API_KEY` (or any other provider) for anything you don't want to lose access to. Run `/login chatgpt` to drive the PKCE flow; the command surfaces this same warning.

## Credits

Caveman prompt-compression rules adapted from [JuliusBrussee/caveman](https://github.com/JuliusBrussee/caveman).

## Screenshots

![bee run](./screenshots/bee-run.png)
![bee skill](./screenshots/bee-skill.png)
![bee hyperplan](./screenshots/bee-hyperplan.png)

## License

MIT.
