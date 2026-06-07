```

                             Ie                á”
                              (ÆÆ           ÌÆÆ­
                                 ÆÆ        ÆÓ
           jÆÆÆÞ             ÆÆ    ÆÆÆä1ÆÆè    ÆÆ              ;‹
           ’„‹ ÆÆ=ÆÆ          Æ    BÆÆÆÆÆÆ    ÆW          ÆÆÆÆÆì»Í~
            îÍ  ?RÆAÆÆÆÆM     ÍÆ  ÆÆ  ‹Ò ÆÆ  ÆÆ       ÆÆÆ‰ ñ    ¯ç–
             «¤’  Æ  å„ «ÆÆÆ    Æ ÆÆÆÆÆÆÆÆÆ ÆÖ    ÆÆÆ®õwÆ ÆP í–¯ª
               íÉ%  +  ¼º  ÛÞÆÆ  Y  üÆÆÆå‚ Æ   ÆÆÆÆ  ;Ø  ú   ïº:
                  oÑQÆÆÆÆÆØÉÝÅBÆÆÆÆÇÆÆÆÆ±ÆÆÆÆÆÆÆÑ  xŸÁÆÆz®ç†
                  ¾Ÿ}sÆÆÞâÆÆÆÆÆÆÆÊÆÆÆÆÆÇÆÇ7Ëà          ’ÆðF
                            ˜      ÇÆÆÆÆÆØZ    ¾ÆÑÆQË§¼÷
                               ÆÆÆÆ ôXÆÆã“sÇÆÆÑ
                            ÆÆÆ    #ÆÄiSœÆÆ    ÆÆÆ
                          ÆÆ  ÆÆTòHÆÆÆÆÆÆÆÆXiÆÆÆ  ÆÆ
                        ÆÆ  ÆÆÍ  l=¡5ÇÇS9¾yï×  —Æ  FÆ
                      ÆÆ   ÆÆ   ¨ÆÆÆÆÆÆÆÆÆÆÆÆ}  ÆÆ   ÆÆ
                     Æ4   3Æ²   »ÇÇoàÇSL…¹ ÇÇ    ÆÆ    Æ¦
                          ÆÆ     GÆÆÆÆÆÆÆÆÆÆÒ7   ËÆ
                          Æ¯      ÆÆæëÚšYJQÄÇ     Æ
                         1Æ       çØÆÆÆÆÆÆÆÇ      Æ6
                        ÆÆ          §ËÆÆÆé         Æ³
                      –ÆÆ                           Æ
```

> "I'm not a bot. I'm a bee. There's a difference."

# bee

[![CI](https://github.com/elhenro/bee/actions/workflows/ci.yml/badge.svg)](https://github.com/elhenro/bee/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/elhenro/bee.svg)](https://pkg.go.dev/github.com/elhenro/bee)
[![Go Report](https://goreportcard.com/badge/github.com/elhenro/bee)](https://goreportcard.com/report/github.com/elhenro/bee)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Release](https://img.shields.io/github/v/release/elhenro/bee)](https://github.com/elhenro/bee/releases)
<img src="./demo/bee.gif" alt="bee demo" />

bee coding agent harness. Pure Go, single static binary, requires Go 1.26+ to build.

```sh
# install (curl | sh)
curl -fsSL https://raw.githubusercontent.com/elhenro/bee/main/install.sh | sh

# or via go install
go install github.com/elhenro/bee/cmd/bee@latest

# or build to your local bin
go build -o ~/.local/bin/bee ./cmd/bee

# use
export OPENROUTER_API_KEY=<REDACTED>
bee
```

## Why?

I've used claude code, codex, hermes, opencode, openclaw and pi. Each nails something. None felt configurable, minimal, or flexible enough for how I work. bee is the harness that just does it.

**Three gaps bee closes:**

1. **Tiny-context friendly.** Caveman-compressed system prompt, three tools, top-k memory. Scales from a 4k-context local Ollama up through small fine-tunes to million-token frontier models. Native [omlx](https://github.com/jundot/omlx) (Apple Silicon MLX server) and OpenRouter support out of the box. Shrinks itself when context gets tight.
2. **Skills are `bee <name>` subcommands.** Write a markdown file, get a command. No shell shims. `bee criticize plan.md` just works, from any directory, in any shell.
3. **Skills are agent endpoints.** A prompt, an external command, an MCP server, or an HTTP endpoint — all four are equally callable tools the model can invoke mid-task. Plug a personal-life agent in as a sub-agent (bundled `hermes.md` is a template). No IPC dance.

**It just works.** bee nudges models toward useful output, detects and breaks loops before they waste tokens, and handles the boring plumbing so you don't have to.

## Local models

bee runs against any OpenAI-compatible local server. Confirmed working:

- **[omlx](https://github.com/jundot/omlx)** (Apple Silicon MLX server, `localhost:8000/v1`) — MLX-quantized coder models, strong tool-calling, low RAM.
- **Ollama** (`localhost:11434/v1`) — `llama3.1:8b`, `qwen2.5-coder:7b`, `qwen3.6:35b`, `gemma4:12b`.
- **LM Studio** (`localhost:1234/v1`).

For sub-8k-context models, switch to the tiny profile. `--profile` is not a CLI flag — set it via env or `~/.bee/config.toml`:

    BEE_PROFILE=tiny bee run --provider omlx --model Qwen3.6-35B-A3B-4bit -- "..."

    # or persist in ~/.bee/config.toml
    profile = "tiny"
    default_provider = "omlx"
    default_model = "Qwen3.6-35B-A3B-4bit"

## Subcommands

`bee run` for headless, `bee` for TUI. Other subcommands:

```sh
$ bee criticize plan.md             # one binary, every skill a subcommand
$ bee run "lint cmd/"               # headless, pipeable
$ bee swarm "migrate auth to jwt"   # queen + workers
$ bee fan "audit internal/ for cleanup"  # parallel fan-out
$ bee zzz "tighten error messages"  # overnight autonomous loop
$ bee agents                        # parallel detached agents
$ bee remote-control                # LAN web relay for remote control
```

`~/.bee/skills/*.md` is your library. Add one, it shows up. First run seeds defaults. Edit one, it lives.

## Config

`~/.bee/config.toml`, sane defaults, set an API key, change models.

## Browser tools

Enable in `~/.bee/config.toml`:

    [browser]
    enabled = true

Or pass `--browser` to any `bee run` invocation, or use the dedicated subcommand:

    bee browse https://example.com

Inside a running TUI session you can toggle browser support live with `/browser on` and `/browser off`. Chrome/Chromium is auto-detected from standard install paths.

Available tools the agent can call:

| Tool                 | What it does                                                            |
| -------------------- | ----------------------------------------------------------------------- |
| `browser_open`       | Navigate to a URL, return the page title and accessibility snapshot     |
| `browser_snapshot`   | Re-snapshot the current page (interactive elements with `[ref]` labels) |
| `browser_console`    | Drain buffered console messages                                         |
| `browser_click`      | Click an element by its `ref` from a snapshot                           |
| `browser_type`       | Type text into an element by `ref`                                      |
| `browser_screenshot` | Capture a screenshot and describe it via a local vision model (opt-in)  |

The snapshot assigns a short `[eN]` ref to every interactive element so the agent can click or type by ref without XPath.

### Vision (screenshot-to-text)

`browser_screenshot` is only registered when a vision model is configured. Point it at a local [Ollama](https://ollama.com) server running a vision model such as `llava`:

    [browser.vision]
    model    = "llava"
    endpoint = "http://localhost:11434"

## Caveman mode

Token-compression rules injected into the system prompt. On by default. `caveman = "auto"` resolves per profile: `full` on `tiny` and `normal`, `lite` on `large`.

Force a level regardless of profile:

    bee --caveman full
    bee run --caveman full -- "..."
    # or set caveman = "full" in ~/.bee/config.toml

Disable:

    bee --caveman off
    # or set caveman = "off" in ~/.bee/config.toml

Explicit value beats profile.

## Overnight loop: `bee zzz`

Hand bee an objective and a budget, walk away, wake up to a branch full of small individually-revertable commits. Each iteration runs one focused change and either commits or `git reset --hard` rolls back on failure — the working tree never stays dirty. A live TUI shows the iteration ledger (🐝 foraging · 🌼 committed · 🍃 noop · 🥀 reset · 💥 failed), token cost, and a sleeping bee at the bottom. Type to nudge the run mid-flight; `/stop` exits gracefully after the current iteration, `/abort` cancels immediately.

    bee zzz "tighten error messages across internal/tools" --max-iterations 30
    bee zzz --list                       # past runs
    bee zzz --resume                     # pick up the most recent
    bee zzz --worktree "..."             # isolate in ~/.bee/zzz/worktrees/<id>
    bee zzz --plain                      # disable the TUI (stdin steering still works)
    bee zzz --max-consecutive-fails 5    # tolerance for transient agent stalls
    bee zzz --hard-error-retries 5       # retries per iter on provider 5xx etc
    bee zzz --notes-tail 5               # cap prior-iter sections echoed into prompts
    bee zzz --gc --gc-worktrees          # prune old runs and their worktrees
    bee zzz --gc --gc-stale-running 168h # also reap runs stuck "running" >7d

Artifacts live in `~/.bee/zzz/runs/<id>/`: `notes.md` per-iter summaries, `events.jsonl` raw timeline, `meta.json` run state, `blocked-<iter>.patch` for any iteration that emitted `BLOCKED:`. Inspired by [gnhf](https://github.com/kunchenguid/gnhf).

**Trust model.** Commits land unsigned by default, use `--sign` to opt back in. Pre-commit hooks run unless `--no-verify` is set. Pushing (`--push`) is opt-in, and failures are recorded in `meta.json` so you can tell what reached the remote. `--yes`/`--yolo` auto-approves dangerous shell commands, so pair it with `--worktree` to contain the blast radius. The CLI prints a warning when `--yes` is used without `--worktree`.

## Parallel agents: `bee agents`

Spawn many bees at once, each on its own git worktree. The overview reuses the chat input — every submitted message starts a fresh detached agent under `~/.bee/agents/worktrees/<id>` on branch `agents/<short>`. Rows show initial prompt, elapsed, tokens up/down, last thought, locked model. `j/k`/arrows navigate, `l`/`→`/`enter` opens an agent fullscreen (existing session view), `m` retries a merge.

    bee agents

When an agent ends its turn with `DONE: <summary>` the coordinator rebases its branch onto `main` and fast-forwards. Conflicts post a resolution prompt back to the agent via the inbox and flip its row to **needs input**; auto-retry every 10s or hit `m` to force a retry. Unmerged worktrees stay highlighted in red until they land.

`/model <name>` and `/provider <name>` set the model used for the next spawn (sticky until changed) — mix and match across agents. Killing `bee agents` does not kill the running children; relaunching reconstructs the overview from `~/.bee/sessions/bg/`.

## My setup / how I run this

**Mac M3 Max (64 GB) -> omlx with an MLX-quantized coder model.**
Runs fast, handles small tasks reliably, doesn't choke on context. Good enough for day-to-day. Local, private, free once the hardware is paid for.

## Platform support

- **macOS / Linux** — first-class. Static binaries published for `darwin/{amd64,arm64}` and `linux/{amd64,arm64}`.
- **Windows** — untested

## Credits

Caveman prompt-compression rules adapted from [JuliusBrussee/caveman](https://github.com/JuliusBrussee/caveman).
