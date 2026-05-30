# Enable Browser Tools Mid-Session (`/browser`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add a `/browser on|off` slash command that registers/removes the native browser tools in a running TUI session without restarting bee, session-only (no config persist).

**Architecture:** Extend the `eng.Rebuild` closure to rebuild the tools registry from current cfg (reusing `buildToolsAsker`). A `/browser` command flips `cfg.Browser.Enabled` in memory and calls `Rebuild`; the existing `appendBrowserTools` gate registers or drops the browser tools.

**Tech Stack:** Go 1.26, existing bee `commands.Side` interface, `internal/tools/browser`, bubbletea TUI.

---

## File Structure

- Modify `cmd/bee/tui.go` — extend `eng.Rebuild` closure to rebuild tools.
- Modify `internal/commands/registry.go` — add `SetBrowserEnabled(on bool) (string, error)` to the `Side` interface.
- Create `internal/commands/builtins_browser.go` — `/browser` command + `registerBrowser`.
- Modify `internal/commands/builtins.go` — call `registerBrowser(r)` and add `/browser` to the `/help` list.
- Modify `internal/tui/side_tools.go` — implement `SetBrowserEnabled` on `tuiSide`.
- Create `internal/commands/builtins_browser_test.go` — arg-parsing tests with a fake Side.
- Create `cmd/bee/rebuild_tools_test.go` — assert a rebuilt registry reflects the browser flag.

---

## Task 1: Extend Rebuild closure to rebuild tools

**Files:**
- Modify: `cmd/bee/tui.go` (the `eng.Rebuild = func(e *loop.Engine) error {...}` at ~line 242)

Context: `cwd`, `storeDir`, `app`, `tuiAsker` are all in scope (declared at tui.go:155-162, `reg` built via `buildToolsAsker(cwd, cfg, prov, storeDir, app, tuiAsker)`). The current closure only rebuilds provider + memory.

- [ ] **Step 1: Read the closure and surrounding scope**

Run: `sed -n '155,165p;242,252p' cmd/bee/tui.go` — confirm `cwd, storeDir, app, tuiAsker` names.

- [ ] **Step 2: Extend the closure**

Replace the existing closure body so it also rebuilds the tools registry:

```go
	eng.Rebuild = func(e *loop.Engine) error {
		newProv, err := buildProvider(e.Cfg)
		if err != nil {
			return err
		}
		// rebuild the tool registry from current cfg so mid-session config
		// changes (e.g. /browser enabling browser tools) take effect without
		// a restart. build into a fresh registry first; only swap on success.
		newReg, err := buildToolsAsker(cwd, e.Cfg, newProv, storeDir, app, tuiAsker)
		if err != nil {
			return err
		}
		e.Provider = newProv
		e.Memory = newKnowledgeAdapter(newProv, e.Cfg)
		e.Tools = newReg
		return nil
	}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/bee/tui.go
git commit -m "feat(tui): rebuild tool registry on engine Rebuild"
```

---

## Task 2: Add SetBrowserEnabled to the Side interface

**Files:**
- Modify: `internal/commands/registry.go` (the `type Side interface` block)

- [ ] **Step 1: Read the Side interface** to find a sensible insertion point (near the other tool/config methods, e.g. after `SetMaxIterations` / near `OpenToolsPane`).

- [ ] **Step 2: Add the method to the interface**

```go
	// SetBrowserEnabled turns the native browser tools on or off for the
	// current session only (no config persist) and rebuilds the tool
	// registry so the change takes effect immediately. Returns a status
	// string for the user. Errors when turning on with no Chrome found.
	SetBrowserEnabled(on bool) (string, error)
```

- [ ] **Step 3: Build (expect failure — tuiSide doesn't implement it yet, but other Side impls/fakes may also need it)**

Run: `go build ./...`
Expected: FAIL — `*tuiSide` (and any test fakes) don't satisfy `Side`. That is fixed in Tasks 3 and 5.

- [ ] **Step 4: Commit after Task 3 compiles** (do not commit a broken build; this task is committed together with Task 3).

---

## Task 3: Implement SetBrowserEnabled on tuiSide

**Files:**
- Modify: `internal/tui/side_tools.go`

Context: `tuiSide` has `s.m.eng` (the `*loop.Engine`), `s.m.eng.Cfg` (a `config.Config` value), and `s.m.eng.Rebuild`. See `AddUserTool` in the same file for the established pattern (mutate `cfg`, call `Rebuild`). Add import `"github.com/elhenro/bee/internal/tools/browser"`.

- [ ] **Step 1: Read `AddUserTool`** in `side_tools.go` to match the guard/rebuild pattern.

- [ ] **Step 2: Implement the method**

```go
// SetBrowserEnabled flips browser-tool support for this session only (no
// persist) and rebuilds the registry so the tools are dispatchable now.
// Turning on requires a detectable Chrome/Chromium. Returns a status line.
func (s *tuiSide) SetBrowserEnabled(on bool) (string, error) {
	if s.m == nil || s.m.eng == nil {
		return "", errors.New("no tui state")
	}
	cfg := &s.m.eng.Cfg
	if on {
		if _, err := browser.DetectChrome(cfg.Browser.ChromePath); err != nil {
			return "", fmt.Errorf("browser: %w (install Chrome/Chromium or set [browser] chrome_path)", err)
		}
	}
	prev := cfg.Browser.Enabled
	if prev == on {
		if on {
			return "browser tools already enabled", nil
		}
		return "browser tools already disabled", nil
	}
	cfg.Browser.Enabled = on
	if s.m.eng.Rebuild != nil {
		if err := s.m.eng.Rebuild(s.m.eng); err != nil {
			cfg.Browser.Enabled = prev // revert on failure
			return "", fmt.Errorf("browser: rebuild: %w", err)
		}
	}
	if on {
		n := browserToolCount(s.m.eng)
		return fmt.Sprintf("browser tools enabled (%d tools). use /tools to fine-tune.", n), nil
	}
	return "browser tools disabled", nil
}

// browserToolCount counts registered tools whose name starts with "browser_".
func browserToolCount(e *loop.Engine) int {
	if e == nil || e.Tools == nil {
		return 0
	}
	n := 0
	for _, name := range e.Tools.Names() {
		if strings.HasPrefix(name, "browser_") {
			n++
		}
	}
	return n
}
```

Ensure imports include `errors`, `fmt`, `strings`, `github.com/elhenro/bee/internal/loop`, and `github.com/elhenro/bee/internal/tools/browser` (check which are already present in the file before adding).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: clean (Side now satisfied by tuiSide). If a test fake elsewhere implements `Side`, it will fail to compile — note it and add a stub `SetBrowserEnabled` there in Task 5.

- [ ] **Step 4: Commit (Tasks 2 + 3 together)**

```bash
git add internal/commands/registry.go internal/tui/side_tools.go
git commit -m "feat(browser): SetBrowserEnabled side method for live toggle"
```

---

## Task 4: The `/browser` command

**Files:**
- Create: `internal/commands/builtins_browser.go`
- Modify: `internal/commands/builtins.go` (call `registerBrowser(r)`, add `/browser` to the `/help` list)
- Test: `internal/commands/builtins_browser_test.go`

- [ ] **Step 1: Write failing test**

First READ an existing `internal/commands/*_test.go` to find the established fake `Side` (e.g. a `fakeSide`/`stubSide` type). If one exists, embed it and override `SetBrowserEnabled`. If none exists, define a minimal local fake implementing only the methods the test exercises is NOT possible (Side is large) — so reuse the existing test fake. The test:

```go
// internal/commands/builtins_browser_test.go
package commands

import (
	"context"
	"strings"
	"testing"
)

func TestBrowserCommand_OnOffBare(t *testing.T) {
	r := NewRegistry()
	registerBrowser(r)
	cmd, ok := r.Get("browser")
	if !ok {
		t.Fatal("browser command not registered")
	}

	fs := &browserFakeSide{}
	// /browser on
	out, err := cmd.Run(context.Background(), []string{"on"}, fs)
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	if !fs.lastOn || !strings.Contains(out, "enabled") {
		t.Errorf("on: lastOn=%v out=%q", fs.lastOn, out)
	}
	// /browser off
	out, err = cmd.Run(context.Background(), []string{"off"}, fs)
	if err != nil {
		t.Fatalf("off: %v", err)
	}
	if fs.lastOn || !strings.Contains(out, "disabled") {
		t.Errorf("off: lastOn=%v out=%q", fs.lastOn, out)
	}
	// bare /browser -> status/usage, must NOT call SetBrowserEnabled
	fs.calls = 0
	if _, err := cmd.Run(context.Background(), nil, fs); err != nil {
		t.Fatalf("bare: %v", err)
	}
	if fs.calls != 0 {
		t.Errorf("bare /browser must not toggle, calls=%d", fs.calls)
	}
}

func TestBrowserCommand_BadArg(t *testing.T) {
	r := NewRegistry()
	registerBrowser(r)
	cmd, _ := r.Get("browser")
	out, err := cmd.Run(context.Background(), []string{"banana"}, &browserFakeSide{})
	if err == nil && !strings.Contains(out, "usage") {
		t.Errorf("bad arg should error or show usage, got %q", out)
	}
}
```

Define `browserFakeSide` in the test file. If reusing an existing fake is cleaner, embed it:

```go
type browserFakeSide struct {
	noopSide // EMBED THE EXISTING TEST FAKE if one exists; otherwise see note below
	lastOn   bool
	calls    int
}

func (f *browserFakeSide) SetBrowserEnabled(on bool) (string, error) {
	f.calls++
	f.lastOn = on
	if on {
		return "browser tools enabled (5 tools)", nil
	}
	return "browser tools disabled", nil
}
```

NOTE for implementer: replace `noopSide` with whatever zero-value `Side` fake the package's existing tests use so the struct satisfies the full `Side` interface. If the package has NO reusable fake, add the embed of the smallest existing one. Do not hand-stub all ~40 Side methods.

- [ ] **Step 2: Run test, expect FAIL** (`registerBrowser` undefined).

Run: `go test ./internal/commands/ -run TestBrowserCommand -v`

- [ ] **Step 3: Implement the command**

```go
// internal/commands/builtins_browser.go
package commands

import (
	"context"
	"fmt"
)

// registerBrowser wires /browser, a live session-only toggle for the native
// browser tools. `/browser on` registers them, `/browser off` removes them,
// bare `/browser` prints usage. Not persisted to config.
func registerBrowser(r *Registry) {
	r.Register(Command{
		Name:        "browser",
		Description: "toggle native browser tools for this session (/browser on|off)",
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if len(args) == 0 {
				return "usage: /browser on | /browser off", nil
			}
			switch args[0] {
			case "on", "enable":
				return s.SetBrowserEnabled(true)
			case "off", "disable":
				return s.SetBrowserEnabled(false)
			default:
				return "", fmt.Errorf("usage: /browser on | /browser off")
			}
		},
	})
}
```

- [ ] **Step 4: Wire into RegisterBuiltins**

In `internal/commands/builtins.go`, add `registerBrowser(r)` alongside the other `register*` calls, and add `/browser` to the `/help` command's list string.

- [ ] **Step 5: Run test, expect PASS**

Run: `go test ./internal/commands/ -run TestBrowserCommand -v`

- [ ] **Step 6: Build + full commands test**

Run: `go build ./... && go test ./internal/commands/`
Expected: clean + green.

- [ ] **Step 7: Commit**

```bash
git add internal/commands/builtins_browser.go internal/commands/builtins.go internal/commands/builtins_browser_test.go
git commit -m "feat(browser): /browser slash command to toggle tools live"
```

---

## Task 5: Rebuild-rebuilds-tools test + fix any Side fakes

**Files:**
- Create: `cmd/bee/rebuild_tools_test.go`
- Modify: any non-TUI `Side` implementation or test fake that now fails to compile (add a `SetBrowserEnabled` stub).

- [ ] **Step 1: Find broken Side implementers**

Run: `go build ./... 2>&1 | head` and `go vet ./... 2>&1 | head`. For each type that no longer satisfies `Side`, add:

```go
func (x *T) SetBrowserEnabled(on bool) (string, error) { return "", nil }
```
(Match the real receiver/type. Headless or fallback Side impls just return empty.)

- [ ] **Step 2: Write the rebuild test**

Verify that building the tool registry with `cfg.Browser.Enabled` true (and a fake chrome_path that passes detection) yields the browser tools, and false omits them. This tests `buildToolsAsker` + `appendBrowserTools` together — the exact path the Rebuild closure uses.

```go
// cmd/bee/rebuild_tools_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestBuildTools_BrowserFlagControlsBrowserTools(t *testing.T) {
	// fake chrome so DetectChrome passes without launching anything
	dir := t.TempDir()
	fake := filepath.Join(dir, "chrome")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	base := config.Config{}
	base.Browser.ChromePath = fake

	// disabled -> no browser tools
	base.Browser.Enabled = false
	regOff, err := buildToolsForTest(t, base)
	if err != nil {
		t.Fatal(err)
	}
	if hasBrowserTool(regOff.Names()) {
		t.Error("browser tools present when disabled")
	}

	// enabled -> browser tools appear
	base.Browser.Enabled = true
	regOn, err := buildToolsForTest(t, base)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBrowserTool(regOn.Names()) {
		t.Error("browser tools missing when enabled")
	}
}

func hasBrowserTool(names []string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, "browser_") {
			return true
		}
	}
	return false
}
```

`buildToolsForTest` is a thin helper the implementer writes that calls the real build path with the minimal deps. INSPECT the signature of `buildToolsWithApprover`/`buildToolsAsker` and the cheapest way to get an `approval.Approver` (e.g. an auto-approve stub) and an `ask.Asker`. If those interfaces are large, prefer calling `buildToolsWithApprover(cwd, cfg, nil-or-stub-provider, "", autoApprover)` if a provider-free path exists; otherwise pass the simplest stubs. If constructing the deps is impractical, instead test `appendBrowserTools` directly (it only needs `[]tools.Tool` and `config.Config`) — that still proves the flag gates the tools:

```go
func buildToolsForTest(t *testing.T, cfg config.Config) (*tools.Registry, error) {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tl := range appendBrowserTools(nil, cfg) {
		if err := reg.Register(tl); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
```
(Import `github.com/elhenro/bee/internal/tools`. This calls the unexported `appendBrowserTools` from Task 1's earlier browser feature, which lives in `cmd/bee/run_tools.go` — same package `main`, so it is reachable.)

- [ ] **Step 3: Run the test**

Run: `go test ./cmd/bee/ -run TestBuildTools_BrowserFlag -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/bee/rebuild_tools_test.go
# plus any Side-fake stub files you had to touch
git commit -m "test(browser): registry reflects browser flag; fix Side fakes"
```

---

## Task 6: Final verification + docs

- [ ] **Step 1: Full build/vet/test/lint**

Run:
```bash
go build ./... && go vet ./... && go test ./... && golangci-lint run
```
Expected: all clean (the env-gated browser integration test still skips).

- [ ] **Step 2: Manual smoke (optional, needs a TTY)**

Build `bee`, start the TUI without `--browser`, type `/browser on`, confirm the status line, then ask the model to use `browser_open` — or check `/tools` lists the browser tools.

- [ ] **Step 3: Document**

Add a line to the README's Browser tools section: browser support can be toggled live in a session with `/browser on` / `/browser off` (session-only, not persisted). No em-dashes.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs(browser): document /browser live toggle"
```

---

## Self-Review Notes

- **Spec coverage:** session-only no-persist (Task 3, no PersistSetting), shared Rebuild rebuilds tools (Task 1), `/browser on|off|bare` (Task 4), Chrome-absent error (Task 3), revert-on-rebuild-failure (Task 3), `/tools` bonus (automatic, no code), tests (Tasks 4, 5). Covered.
- **Type consistency:** `SetBrowserEnabled(on bool) (string, error)` identical across interface (Task 2), impl (Task 3), command call (Task 4), fakes (Task 5). `browserToolCount` / `hasBrowserTool` helpers named consistently.
- **Open verification points for the implementer:** the exact existing `Side` test fake to embed (Task 4 Step 1), which `Side` implementations break and need a stub (Task 5 Step 1), and whether `appendBrowserTools` is reachable from the `cmd/bee` test (it is — same package `main`).
