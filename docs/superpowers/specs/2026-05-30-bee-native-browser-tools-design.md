# Native Browser Tools for bee (Phase 1)

**Date:** 2026-05-30
**Status:** Approved, pending implementation plan

## Problem

bee agents driving small local models (e.g. `omlx/Qwen3-Coder-Next-4bit`) have no way to open the website or game they are building, observe its console, or click around. They are blind to the runtime behaviour of their own output. We want a simple, text-first browser loop that works even on models without vision, with optional vision via a local sidecar model.

## Constraints

- Stay pure-Go single binary (bee's first wedge). New dep must be CGO-free.
- Drive an existing Chrome/Chromium install; do not bundle a browser.
- Keep tool surface lean for tiny-context profiles: browser tools are opt-in, off by default.
- Each new source file stays under ~200 lines.
- Vision is optional. The core navigate/snapshot/click/console loop must work with a text-only model.

## Approach

Native Go browser tools backed by `github.com/chromedp/chromedp` (pure-Go CDP driver, no CGO). Chosen over wiring the dormant `KindMCP` skill runtime (which would add a Node/`npx` dependency and break single-binary purity) and over `go-rod` (chromedp is the canonical, most-used driver). The dead `KindMCP` stub is left untouched; resurrecting it is a separate future effort.

## Architecture

New package `internal/tools/browser` owning one lazy-launched chromedp session and exposing 5 tools. A vision sidecar client handles screenshot-to-text. A `bee browse <url>` subcommand plus a bundled `browse.md` recipe provide the autonomous loop. Two config blocks gate everything.

Files (each <200 lines):

- `detect.go` — locate a Chrome/Chromium binary (config override, then known macOS/Linux paths, then `$PATH`).
- `session.go` — `*Session`: chromedp context + cancel funcs, lazy launch, console ring buffer, shutdown.
- `snapshot.go` — accessibility-tree fetch, trim, stable-ref assignment, ref→node resolution.
- `tools.go` — the 5 `tools.Tool` implementations (`Spec()` + `Run()`).
- `vision.go` — ollama `/api/generate` client for screenshot description.

## Session lifecycle

- A single `*Session` holds the chromedp context and cancel funcs plus the console ring buffer and the current ref→backendNodeID map.
- **Lazy launch:** registering the tools does not open Chrome. The first tool call launches it. Reused across the run; closed on engine shutdown.
- Headful by default (`headless=false`) so the user can watch the page live. Config/flag switches to headless for CI.
- Drives the *existing* detected Chrome via a chromedp exec-allocator pointed at the binary path. No browser is downloaded or bundled.
- Console messages are captured by subscribing to CDP `Runtime.consoleAPICalled` and `Log.entryAdded` events, appended to a bounded ring buffer. `browser_console` drains messages logged since the previous read.

## Tools

| Tool | Input | Returns |
|---|---|---|
| `browser_open` | `url` | page title + accessibility snapshot |
| `browser_snapshot` | — | current accessibility tree |
| `browser_console` | — | console messages since last call (level + text) |
| `browser_click` | `ref` | clicks node by ref, returns fresh snapshot + console tail |
| `browser_type` | `ref`, `text` | focuses node, types text, returns fresh snapshot |
| `browser_screenshot` | `question?` | captures PNG, sends to vision model, returns text description. Registered only when `[browser.vision]` is configured. |

## Snapshot format

Fetched via `Accessibility.getFullAXTree`, trimmed to interactive/meaningful nodes, rendered as text with stable refs. The session maps each ref to a backend node ID so `browser_click(ref)` / `browser_type(ref, …)` resolve without re-querying by selector.

```
- button "Login" [e5]
- textbox "Email" [e6]
- link "Sign up" [e7]
```

This is the key mechanism that lets a no-vision model navigate: it reads roles/names/refs as text and acts by ref.

## Vision client

`browser_screenshot` captures a PNG, base64-encodes it, and POSTs `{model, prompt, images:[b64]}` to the configured ollama `/api/generate` endpoint, returning the model's text answer. The main driving model never receives pixels — it gets a text description as the tool result, sidestepping the fact that `ToolResult.Content` is text-only. Request is timeout-bounded; if the endpoint is down the tool returns `IsError` without killing the run.

## Configuration

```toml
[browser]
enabled = false        # master gate; also enabled per-run by --browser flag
headless = false       # headful by default
chrome_path = ""       # auto-detect when empty

[browser.vision]
model = "llava"                       # unset -> screenshot tool not registered
endpoint = "http://localhost:11434"
```

New config structs: `BrowserConfig` (with nested `Vision BrowserVisionConfig`) added to `config.Config`.

## Tool registration

In `buildToolsWithApprover` (`cmd/bee/run_tools.go`): when `cfg.Browser.Enabled` or the `--browser` flag is set, run Chrome detection. If a binary is found, register the 5 browser tools (lazy — no launch yet). The `browser_screenshot` tool is appended only when `cfg.Browser.Vision.Model` is non-empty. If Chrome is not found, registration is skipped silently. Browser tools also respect the existing `disabled_tools` mechanism.

## browse subcommand + recipe

- `bee browse <url>` added to the `cmd/bee/main.go` dispatch switch. It force-enables browser tools for that run, seeds a prompt instructing the model to open the URL, snapshot, report any console errors, and await further instructions, then runs the standard engine.
- A bundled `browse.md` recipe skill (in `internal/skills/bundled`) sequences `browser_open` → `browser_snapshot` → `browser_console` so the open/observe loop is reproducible and model-callable mid-task.
- If Chrome is absent, `bee browse` exits with an actionable install hint.

## Error handling

- Chrome binary not found: tools not registered; `bee browse` errors with an install hint.
- Launch or navigation failure: tool returns `IsError` with the message; the run continues.
- Vision endpoint unreachable or erroring: only `browser_screenshot` returns `IsError`; other tools and the run are unaffected.
- Navigation and vision calls are bounded by context timeouts so a hung page cannot wedge the agent.

## Testing

- **Unit:** ref-assignment and ref→node resolution in `snapshot.go`; console ring-buffer drain semantics; vision client against an `httptest` stub server.
- **Integration:** chromedp driven against a local test HTML page (served from a `data:` URL or temp file), gated by a `BEE_BROWSER_TEST` env var so CI machines without Chrome skip cleanly. Assertions: snapshot contains a known button with a ref; clicking that ref mutates page state observable in the next snapshot; a `console.log` in the test page surfaces via `browser_console`.
- No network needed in CI: vision is stubbed via `httptest`; chromedp integration is env-gated.

## Dependency

- `github.com/chromedp/chromedp` — pure-Go, no CGO. Single-binary property preserved.

## Out of scope (later phases)

- Resurrecting the `KindMCP` runtime to consume real MCP servers (Playwright MCP et al.).
- File upload, drag, multi-tab orchestration, network-request inspection.
- Piping real image blocks into tool results (would require extending `ToolResult`).
