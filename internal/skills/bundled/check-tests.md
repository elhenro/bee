---
name: check-tests
type: recipe
description: Run the Go test suite, commit if green, explain and escalate if red.
steps:
  - id: changed
    kind: shell
    description: List files changed since the last commit so the user sees scope.
    run: git status --short
  - id: tests
    kind: shell
    description: Run the full Go test suite.
    run: go test ./...
    on_failure: explain
    next: stage
  - id: explain
    kind: bee
    description: Summarize what is broken, then hand back to the user.
    prompt: |
      These tests failed:
      {{tests}}

      Summarize what is broken in one short paragraph. Do not fix anything.
    next: escalate
  - id: stage
    kind: shell
    description: Stage the changed files for commit.
    run: git add -A
  - id: commit
    kind: bee
    description: Write a conventional commit for the staged changes.
    tools: [bash]
    prompt: |
      Changed files:
      {{changed}}

      Write a Conventional Commits subject (max 50 chars) for the staged
      changes and run `git commit -m "<subject>"` with the bash tool.
      Reply with the subject only.
---
Runtime recipe: shell steps run in Go through the gated bash tool; the two
bee steps are the only model turns. Invoke as `bee check-tests`.
