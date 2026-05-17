# Contributing to bee

Thanks for the interest. bee is small, opinionated, and deliberately
minimal. Keep contributions in that spirit.

## Repo layout

```
cmd/bee/         entry, headless, TUI, fan, swarm, install-shims
internal/loop/   agent turn loop
internal/tools/  apply_patch, shell, read
internal/skills/ parse, shim, watcher, registry, bundled defaults
internal/knowledge/ on-disk frontmatter knowledge store (scan + query + write)
internal/llm/    OpenAI-compatible provider + wire translation
internal/caveman/ embedded prompt-injection rules
internal/sandbox/ two-axis policy + sandbox-exec/bwrap
internal/session/ JSONL rollouts + parent-pointer tree
internal/hive/   fan-out + queen-and-workers
internal/tui/    bubbletea app
internal/config/ TOML config + profiles
internal/types/  agent-owned message/tool types
```

Each internal package is independently testable, doesn't import siblings
except via clearly-named entry points, and respects the ≤300-lines-per-file
convention.

## Build & test

```sh
go build ./...
go vet ./...
go test ./...
```

CI runs the same on Linux, macOS, Windows. golangci-lint is also run via
`.golangci.yml`.

## Local development

```sh
# interactive against your real provider
go run ./cmd/bee

# offline / deterministic
BEE_TEST_PROVIDER=stub go run ./cmd/bee run --headless "hi"

# point bee at an isolated home (does not touch ~/.bee)
BEE_HOME=/tmp/bee-dev go run ./cmd/bee install-shims
```

## Style

- Code: ≤300 lines per Go file. Split before you bloat.
- Comments: lowercase, minimal, skip articles. Only when non-obvious.
- Errors: wrap with `%w` and a short call-site identifier
  (`fmt.Errorf("foo: %w", err)`). Never swallow without a reason.
- Imports grouped stdlib / third-party / project.
- Tests next to code as `*_test.go`. Prefer table-driven.
- No new deps unless absolutely necessary. Keep the dep tree small.

## Commits

Conventional Commits. Subject ≤50 chars, imperative.

```
feat(skills): add http skill kind
fix(loop): cancel context on tool error
docs(readme): tighten three-wedges section
```

Body only when the "why" is non-obvious. Skip otherwise.

## PRs

- One topic per PR. If the diff outgrows the description, split it.
- Include a "Test plan" section. List the commands you ran and what
  passed.
- Pass `go test ./...` and `golangci-lint run` before requesting review.
- Re-run `go vet` after touching any file.

## Issues

Bugs: include repro, `bee version`, the failing command, and a stderr
excerpt. Feature requests: open a discussion first if it's bigger than
one file's worth of code.

## License

By contributing you agree your work is released under the MIT License,
same as the rest of the project.
