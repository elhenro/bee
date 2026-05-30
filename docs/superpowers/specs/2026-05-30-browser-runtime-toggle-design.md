# Enable Browser Tools Mid-Session (`/browser`)

**Date:** 2026-05-30
**Status:** Approved, pending implementation

## Problem

Browser tools are gated by `cfg.Browser.Enabled`, evaluated once at startup when `buildToolsAsker` builds the tool registry. A TUI session started without `--browser` (or `[browser] enabled`) has zero browser tools in `eng.Tools`, and the `/tools` pane cannot surface them because it only lists already-registered tools. There is no way to turn browser support on without restarting bee.

## Constraints

- Session-only: enabling via `/browser` must NOT persist to `config.toml`. Restart returns to the configured default.
- Reuse the existing `appendBrowserTools` gate; no duplicate registration logic.
- Follow existing slash-command and `Side` patterns. Files stay under ~200 lines.

## Root cause

`eng.Rebuild` (`cmd/bee/tui.go`) only rebuilds the provider and memory adapter, not the tools registry. So no mechanism re-registers tools mid-session. (This also means the existing `AddUserTool` live-add does not actually become dispatchable until restart, a latent bug fixed as a side effect here.)

## Approach

Extend `eng.Rebuild` to also rebuild the tools registry from current cfg, then add a `/browser` command that flips `cfg.Browser.Enabled` in memory and calls `Rebuild`. The existing gate registers or drops the browser tools automatically.

## Components

1. **Rebuild closure** (`cmd/bee/tui.go`): capture `cwd, storeDir, app, tuiAsker`. After rebuilding the provider, build a fresh registry via `buildToolsAsker(cwd, e.Cfg, newProv, storeDir, app, tuiAsker)` and assign `e.Tools`. On build error, return it without mutating `e.Tools`.

2. **`Side.SetBrowserEnabled(on bool) (string, error)`** (`internal/commands/registry.go` + `internal/tui/side_tools.go`): when turning on, detect Chrome via `browser.DetectChrome(cfg.Browser.ChromePath)`; if absent, return an error with an install hint and do not flip the flag. Otherwise set `cfg.Browser.Enabled = on` in memory, call `eng.Rebuild(eng)`, and return a status string. No `PersistSetting` call (session-only).

3. **`/browser` command** (`internal/commands/builtins_browser.go`): `/browser on`, `/browser off`, bare `/browser` reports current state. Parses the arg, calls `s.SetBrowserEnabled`, returns the status text rendered to scrollback.

## Data flow

`/browser on` -> command -> `s.SetBrowserEnabled(true)` -> tuiSide detects Chrome, sets `cfg.Browser.Enabled=true`, `eng.Rebuild(eng)` -> new registry includes `browser_open`, `browser_snapshot`, `browser_console`, `browser_click`, `browser_type` (plus `browser_screenshot` when `[browser.vision]` model set) -> next turn the model sees them. Status echoed, e.g. `browser tools enabled (6 tools)`.

## Error handling

- Chrome absent while turning on: return error, flag unchanged, hint to install Chrome/Chromium.
- `eng.Rebuild` fails: propagate the error; revert the flag to its prior value so state stays consistent.
- No engine / headless context: return a graceful error string instead of panicking.

## Bonus

Once registered, browser tools appear in the `/tools` pane (reads `eng.Tools.Specs()`), so individual tools can be disabled there.

## Known tradeoff

Each enable rebuilds the registry, creating a fresh `*Session`. Toggling off then on after the browser launched leaks the prior Chrome process until bee exits. Acceptable for infrequent session-only toggling; consistent with the Phase-1 session-lifecycle debt.

## Testing

- `/browser` arg parsing: on, off, bare (status), and an invalid arg. Pure unit test in `commands` with a fake `Side`.
- Rebuild rebuilds tools: a `cmd/bee` test that flips `cfg.Browser.Enabled` and asserts the rebuilt registry gains the browser tool names (using a stub approver/asker and a fake Chrome path so detection passes without launching).

## Out of scope

- Persisting the toggle (deliberately session-only).
- Fixing the broader browser session-close lifecycle (tracked with Phase 1).
