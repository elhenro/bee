---
name: criticize
type: exec
description: shell out to `bee hyperplan` to stress-test a plan before execution
exec: ["bee", "hyperplan"]
stream: true
---
# criticize skill

Wraps the built-in `bee hyperplan` subcommand. Use when a plan exists
and you want it attacked before any code lands: missing steps, hidden
coupling, weak rollback, untested edges.

Invocation:
  `bee criticize <plan-text-or-path>`
  any extra argv flows through to `bee hyperplan` unchanged.

Output streams straight from the underlying command. Named `criticize`
so it doesn't shadow the reserved `bee hyperplan` subcommand on `$PATH`.
