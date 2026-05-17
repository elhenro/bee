# 🐝 bee

![bee](./bee.png)

bee coding agent harness

```sh
# install
go build -o /usr/local/bin/bee ./cmd/bee

# use
export OPENROUTER_API_KEY=...
bee
```

## Why?

Three wedges incumbents miss:

1. **Skills are `bee <name>` subcommands.** Write a markdown file, get a command. No shell shims. No REPL incantations. `bee calc` just works, from any directory, in any shell.
2. **Skills are agent endpoints.** A prompt, an external command, an MCP server, or an HTTP endpoint, all four are equally callable tools the model can invoke mid-task. Your daily driver Hermes runs as a sub-agent. No IPC dance.
3. **Tiny-context friendly.** Caveman-compressed system prompt, three tools, top-k memory. Same harness scales from a 4k-context local Ollama to small fine-tunes like Qwen3.6-35B-A3B-4bit up to DeepSeek v4 Flash's 1M window.

Built light. Shrinks itself when context gets tight. Stays small.

## Quick demos

```sh
$ bee calc          # one binary, every skill a subcommand
$ bee run "lint cmd/"   # headless, pipeable
$ bee swarm "migrate auth to jwt"  # queen + workers
$ bee fan "audit internal/ for cleanup"  # parallel fan-out
```

`~/.bee/skills/*.md` is your library. Add one, it shows up. First run seeds defaults. Edit one, it lives.

## Config

`~/.bee/config.toml`, sane defaults, set an API key, change models.

## Local models

bee runs against any OpenAI-compatible local server. Confirmed working:

- **omlx** (Apple Silicon MLX, `localhost:8000/v1`) with `Qwen3.6-35B-A3B-4bit` — strong tool-calling, ~22 GB unified memory.
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

**Mac M3 Max (64 GB) -> omlx, Qwen3.6-35B-A3B-4bit.**
Runs fast, handles small tasks reliably, doesn't choke on context. Good enough for day-to-day. Local, private, free once the hardware is paid for.

## Credits

Caveman prompt-compression rules adapted from [JuliusBrussee/caveman](https://github.com/JuliusBrussee/caveman).

## License

MIT.
