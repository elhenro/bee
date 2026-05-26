---
name: check-tests
type: recipe
description: Run the Go test suite, commit if green, escalate if red.
steps:
  - id: list-changed
    description: List files changed since the last commit so the user sees scope.
    tool: bash
    args:
      command: "git status --short"
  - id: run-tests
    description: Run the full Go test suite.
    tool: bash
    args:
      command: "go test ./..."
    on_failure: explain-failure
  - id: explain-failure
    description: Read the failing test output, summarize what's broken in one short paragraph, then escalate.
    on_failure: escalate
  - id: stage
    description: Stage the changed files for commit.
    tool: bash
    args:
      command: "git add -A"
  - id: commit
    description: Create a single conventional commit describing what changed.
    tool: bash
    args:
      command: "git commit -m \"chore: run tests\""
---
