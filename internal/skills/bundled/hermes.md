---
name: hermes
type: exec
description: personal life agent (calendar, email, notes) — adjust exec path
exec: ["hermes", "--headless"]
stream: true
---
# hermes skill template

This is a template. Edit the `exec:` line above to point at your
Hermes (or any personal-agent) binary. The default `hermes` assumes
the binary is on `$PATH`; if yours lives elsewhere use an absolute
path, e.g. `exec: ["/Users/you/.local/bin/hermes", "--headless"]`.

When invoked, bee spawns the configured command, pipes the user
message on stdin, and streams stdout back. Args after `--` flow
through as additional argv.

Delete this file if you don't use a personal agent.
