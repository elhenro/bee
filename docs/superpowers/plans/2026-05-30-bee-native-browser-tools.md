# Native Browser Tools for bee (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give bee agents (incl. no-vision local models) a text-first browser loop — open a page, read an accessibility-style snapshot, click/type by ref, read the console, and optionally describe a screenshot via a local vision model.

**Architecture:** A new `internal/tools/browser` package owns one lazy-launched chromedp session driving the user's existing Chrome/Chromium (headful by default). Snapshots are produced by injecting JS that tags interactive DOM nodes with `data-bee-ref` attributes and returns a text tree; clicks/types resolve those refs via `[data-bee-ref='eN']` selectors. A vision sidecar POSTs screenshots to a local ollama endpoint and returns text. Tools are opt-in via `[browser]` config or a `--browser` flag; a `bee browse <url>` subcommand and bundled `browse.md` recipe drive the loop.

**Tech Stack:** Go 1.26, `github.com/chromedp/chromedp` (pure-Go CDP, no CGO), existing bee `tools.Tool` interface, TOML config, `net/http` for the ollama client.

---

## File Structure

- Create `internal/config/browser.go` — `BrowserConfig` + `BrowserVisionConfig` structs (or add to `types.go`; new file keeps `types.go` from growing).
- Modify `internal/config/types.go` — add `Browser BrowserConfig` field to `Config`.
- Create `internal/tools/browser/detect.go` — locate a Chrome/Chromium binary.
- Create `internal/tools/browser/session.go` — `*Session`: chromedp ctx, lazy launch, console ring buffer, shutdown.
- Create `internal/tools/browser/snapshot.go` — injected-JS snapshot text + the JS source constant.
- Create `internal/tools/browser/vision.go` — ollama `/api/generate` client.
- Create `internal/tools/browser/tools.go` — the 5/6 `tools.Tool` impls wrapping a shared `*Session`.
- Create `internal/tools/browser/*_test.go` — unit + env-gated integration tests.
- Modify `cmd/bee/run.go` — add `--browser` flag that flips `cfg.Browser.Enabled`.
- Modify `cmd/bee/run_tools.go` — register browser tools when enabled and Chrome detected.
- Create `cmd/bee/browse.go` — `bee browse <url>` subcommand.
- Modify `cmd/bee/main.go` — dispatch `case "browse"`.
- Create `internal/skills/bundled/browse.md` — recipe skill (open → snapshot → console).

Note on snapshot approach: the spec mentioned `Accessibility.getFullAXTree`. This plan instead injects JS to build the same text contract (`- role "name" [ref]`) and tag nodes with `data-bee-ref`, because it makes click/type resolution trivial and avoids low-level cdproto AX wrangling. Same external contract, simpler internals.

---

## Task 1: Add browser config structs

**Files:**
- Create: `internal/config/browser.go`
- Modify: `internal/config/types.go` (add `Browser BrowserConfig` field to `Config`)
- Test: `internal/config/browser_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/browser_test.go
package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestBrowserConfig_Defaults(t *testing.T) {
	var c Config
	if c.Browser.Enabled {
		t.Error("Browser.Enabled should default false")
	}
	if c.Browser.Headless {
		t.Error("Browser.Headless should default false (headful)")
	}
}

func TestBrowserConfig_TOML(t *testing.T) {
	const src = `
[browser]
enabled = true
headless = true
chrome_path = "/custom/chrome"

[browser.vision]
model = "llava"
endpoint = "http://localhost:11434"
`
	var c Config
	if _, err := toml.Decode(src, &c); err != nil {
		t.Fatal(err)
	}
	if !c.Browser.Enabled || !c.Browser.Headless {
		t.Errorf("flags not decoded: %+v", c.Browser)
	}
	if c.Browser.ChromePath != "/custom/chrome" {
		t.Errorf("chrome_path: %q", c.Browser.ChromePath)
	}
	if c.Browser.Vision.Model != "llava" || c.Browser.Vision.Endpoint != "http://localhost:11434" {
		t.Errorf("vision not decoded: %+v", c.Browser.Vision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestBrowserConfig -v`
Expected: FAIL — `c.Browser` undefined (build error).

- [ ] **Step 3: Create the structs**

```go
// internal/config/browser.go
package config

// BrowserConfig gates and configures the native browser tools. Off by
// default to keep the tool surface lean for tiny-context profiles; flip via
// config or the --browser flag.
type BrowserConfig struct {
	Enabled    bool                `toml:"enabled"`
	Headless   bool                `toml:"headless"`    // default false: headful so the user can watch
	ChromePath string              `toml:"chrome_path"` // empty -> auto-detect
	Vision     BrowserVisionConfig `toml:"vision"`
}

// BrowserVisionConfig points browser_screenshot at a local vision model.
// Model empty -> the screenshot tool is not registered.
type BrowserVisionConfig struct {
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint"` // ollama base url, e.g. http://localhost:11434
}
```

Then add to `internal/config/types.go` inside `type Config struct` (next to `UserTools`):

```go
	// Browser gates the native chromedp-backed browser tools.
	Browser BrowserConfig `toml:"browser"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestBrowserConfig -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/browser.go internal/config/types.go internal/config/browser_test.go
git commit -m "feat(config): add [browser] config block"
```

---

## Task 2: Chrome binary detection

**Files:**
- Create: `internal/tools/browser/detect.go`
- Test: `internal/tools/browser/detect_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/tools/browser/detect_test.go
package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectChrome_HonorsOverride(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "chrome")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DetectChrome(fake)
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Errorf("override ignored: got %q want %q", got, fake)
	}
}

func TestDetectChrome_OverrideMissing(t *testing.T) {
	if _, err := DetectChrome("/no/such/chrome"); err == nil {
		t.Error("expected error for missing override path")
	}
}

func TestDetectChrome_NoOverrideReturnsResult(t *testing.T) {
	// On a machine with no browser this returns ErrNotFound; with one it
	// returns a path. Either is acceptable — we only assert it doesn't panic
	// and the error (if any) is ErrNotFound.
	_, err := DetectChrome("")
	if err != nil && err != ErrNotFound {
		t.Errorf("unexpected error type: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/browser/ -run TestDetectChrome -v`
Expected: FAIL — package/function not defined.

- [ ] **Step 3: Implement detection**

```go
// internal/tools/browser/detect.go

// Package browser provides native chromedp-backed browser tools so bee
// agents can open, snapshot, click, and read the console of the page they
// are building. Drives an existing Chrome/Chromium install; never bundles a
// browser. Opt-in via [browser] config or --browser.
package browser

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// ErrNotFound means no Chrome/Chromium binary could be located.
var ErrNotFound = errors.New("browser: no Chrome/Chromium binary found")

// DetectChrome returns a usable browser binary path. override wins when
// non-empty and existing; otherwise known install locations and $PATH are
// probed. Returns ErrNotFound if nothing is usable.
func DetectChrome(override string) (string, error) {
	if override != "" {
		if isExec(override) {
			return override, nil
		}
		return "", errors.New("browser: chrome_path does not exist or is not executable: " + override)
	}
	for _, p := range candidatePaths() {
		if isExec(p) {
			return p, nil
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}

func candidatePaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	}
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/browser/ -run TestDetectChrome -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/browser/detect.go internal/tools/browser/detect_test.go
git commit -m "feat(browser): chrome binary detection"
```

---

## Task 3: Add chromedp dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dep**

Run: `go get github.com/chromedp/chromedp@latest`
Expected: `go.mod` gains `github.com/chromedp/chromedp` and transitive `chromedp/cdproto`, `chromedp/sysutil`, `gobwas/ws`, etc.

- [ ] **Step 2: Verify build still clean**

Run: `go build ./...`
Expected: builds clean (no usage yet).

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add chromedp dependency"
```

---

## Task 4: Session — lazy launch + console ring buffer

**Files:**
- Create: `internal/tools/browser/session.go`
- Test: `internal/tools/browser/session_test.go`

The Session owns the chromedp context, launches Chrome lazily on first use, and accumulates console messages into a bounded ring buffer drained by `browser_console`. Console capture and ring-buffer drain are unit-testable without a real browser by testing the ring buffer directly.

- [ ] **Step 1: Write the failing test (ring buffer only — no browser needed)**

```go
// internal/tools/browser/session_test.go
package browser

import "testing"

func TestConsoleRing_DrainReturnsAndClears(t *testing.T) {
	s := &Session{}
	s.pushConsole("log", "hello")
	s.pushConsole("error", "boom")
	got := s.drainConsole()
	if len(got) != 2 {
		t.Fatalf("want 2 msgs, got %d", len(got))
	}
	if got[0].Level != "log" || got[0].Text != "hello" {
		t.Errorf("msg0 wrong: %+v", got[0])
	}
	if len(s.drainConsole()) != 0 {
		t.Error("drain should clear the buffer")
	}
}

func TestConsoleRing_CapsAtMax(t *testing.T) {
	s := &Session{}
	for i := 0; i < consoleRingMax+50; i++ {
		s.pushConsole("log", "x")
	}
	if got := len(s.drainConsole()); got != consoleRingMax {
		t.Errorf("ring not capped: got %d want %d", got, consoleRingMax)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/browser/ -run TestConsoleRing -v`
Expected: FAIL — `Session`, `pushConsole`, `drainConsole`, `consoleRingMax` undefined.

- [ ] **Step 3: Implement Session**

```go
// internal/tools/browser/session.go
package browser

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const consoleRingMax = 500

// ConsoleMsg is one captured console entry.
type ConsoleMsg struct {
	Level string
	Text  string
}

// Session owns a single chromedp browser context, launched lazily on first
// use and reused across tool calls. Safe for sequential tool calls within a
// run (the engine drives tools one at a time).
type Session struct {
	chromePath string
	headless   bool

	mu        sync.Mutex
	allocCtx  context.Context
	allocStop context.CancelFunc
	ctx       context.Context
	ctxStop   context.CancelFunc
	started   bool

	consoleMu sync.Mutex
	console   []ConsoleMsg
}

// NewSession returns an unstarted session. Chrome launches on first ensure().
func NewSession(chromePath string, headless bool) *Session {
	return &Session{chromePath: chromePath, headless: headless}
}

// ensure launches Chrome once and wires console capture. Subsequent calls are
// no-ops. Caller must hold s.mu.
func (s *Session) ensure() error {
	if s.started {
		return nil
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(s.chromePath),
		chromedp.Flag("headless", s.headless),
	)
	s.allocCtx, s.allocStop = chromedp.NewExecAllocator(context.Background(), opts...)
	s.ctx, s.ctxStop = chromedp.NewContext(s.allocCtx)
	// force the browser process to spin up now so launch errors surface here.
	if err := chromedp.Run(s.ctx); err != nil {
		s.allocStop()
		return fmt.Errorf("browser launch failed: %w", err)
	}
	s.listenConsole()
	s.started = true
	return nil
}

// listenConsole subscribes to console + log events on the target.
func (s *Session) listenConsole() {
	chromedp.ListenTarget(s.ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			var b []byte
			for _, a := range e.Args {
				if len(a.Value) > 0 {
					b = append(b, a.Value...)
					b = append(b, ' ')
				}
			}
			s.pushConsole(string(e.Type), string(b))
		case *log.EventEntryAdded:
			s.pushConsole(string(e.Entry.Level), e.Entry.Text)
		}
	})
}

func (s *Session) pushConsole(level, text string) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	s.console = append(s.console, ConsoleMsg{Level: level, Text: text})
	if len(s.console) > consoleRingMax {
		s.console = s.console[len(s.console)-consoleRingMax:]
	}
}

func (s *Session) drainConsole() []ConsoleMsg {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	out := s.console
	s.console = nil
	return out
}

// run executes chromedp actions against the (lazily launched) context.
func (s *Session) run(ctx context.Context, actions ...chromedp.Action) error {
	s.mu.Lock()
	if err := s.ensure(); err != nil {
		s.mu.Unlock()
		return err
	}
	runCtx := s.ctx
	s.mu.Unlock()
	return chromedp.Run(runCtx, actions...)
}

// Close tears down the browser. Safe to call when never started.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctxStop != nil {
		s.ctxStop()
	}
	if s.allocStop != nil {
		s.allocStop()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/browser/ -run TestConsoleRing -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/browser/session.go internal/tools/browser/session_test.go
git commit -m "feat(browser): lazy session + console ring buffer"
```

---

## Task 5: Snapshot — injected JS that tags refs and returns text

**Files:**
- Create: `internal/tools/browser/snapshot.go`
- Test: `internal/tools/browser/snapshot_test.go`

The snapshot JS walks the DOM, picks interactive/meaningful nodes (links, buttons, inputs, headings, nav landmarks), stamps each with `data-bee-ref="eN"`, and returns a newline list `- role "name" [eN]`. `browser_click`/`browser_type` then select `[data-bee-ref='eN']`. We unit-test the JS string is well-formed (non-empty, references the ref attribute and known roles) and that `snapshotJS` is a stable constant; full behaviour is covered by the env-gated integration test in Task 9.

- [ ] **Step 1: Write the failing test**

```go
// internal/tools/browser/snapshot_test.go
package browser

import "strings"

import "testing"

func TestSnapshotJS_WellFormed(t *testing.T) {
	if snapshotJS == "" {
		t.Fatal("snapshotJS empty")
	}
	for _, must := range []string{"data-bee-ref", "button", "return", "function"} {
		if !strings.Contains(snapshotJS, must) {
			t.Errorf("snapshotJS missing %q", must)
		}
	}
}

func TestRefAttr(t *testing.T) {
	if refAttr != "data-bee-ref" {
		t.Errorf("refAttr = %q", refAttr)
	}
	if got := refSelector("e5"); got != "[data-bee-ref='e5']" {
		t.Errorf("refSelector = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/browser/ -run 'TestSnapshotJS|TestRefAttr' -v`
Expected: FAIL — `snapshotJS`, `refAttr`, `refSelector` undefined.

- [ ] **Step 3: Implement snapshot**

```go
// internal/tools/browser/snapshot.go
package browser

import (
	"context"
	"fmt"

	"github.com/chromedp/chromedp"
)

const refAttr = "data-bee-ref"

func refSelector(ref string) string {
	return "[" + refAttr + "='" + ref + "']"
}

// snapshotJS walks the DOM, tags interactive/meaningful nodes with a stable
// ref attribute, and returns a text outline. Roles are derived from tag +
// aria-role; names from aria-label, placeholder, value, or trimmed text.
const snapshotJS = `
(function () {
  function roleOf(el) {
    var r = el.getAttribute('role');
    if (r) return r;
    var t = el.tagName.toLowerCase();
    if (t === 'a') return 'link';
    if (t === 'button') return 'button';
    if (t === 'input') return (el.type || 'text');
    if (t === 'textarea') return 'textbox';
    if (t === 'select') return 'combobox';
    if (/^h[1-6]$/.test(t)) return 'heading';
    if (t === 'nav') return 'navigation';
    return t;
  }
  function nameOf(el) {
    return (el.getAttribute('aria-label') ||
      el.getAttribute('placeholder') ||
      el.value ||
      (el.innerText || '').trim().slice(0, 80) || '').replace(/\s+/g, ' ').trim();
  }
  var sel = 'a,button,input,textarea,select,[role],h1,h2,h3,nav';
  var nodes = document.querySelectorAll(sel);
  var out = [];
  var n = 0;
  nodes.forEach(function (el) {
    var rect = el.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) return; // skip hidden
    var ref = 'e' + (++n);
    el.setAttribute('data-bee-ref', ref);
    out.push('- ' + roleOf(el) + ' "' + nameOf(el) + '" [' + ref + ']');
  });
  return out.join('\n');
})()
`

// snapshot evaluates snapshotJS in the page and returns the text outline.
func (s *Session) snapshot(ctx context.Context) (string, error) {
	var out string
	if err := s.run(ctx, chromedp.Evaluate(snapshotJS, &out)); err != nil {
		return "", fmt.Errorf("snapshot failed: %w", err)
	}
	if out == "" {
		out = "(no interactive elements found)"
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/browser/ -run 'TestSnapshotJS|TestRefAttr' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/browser/snapshot.go internal/tools/browser/snapshot_test.go
git commit -m "feat(browser): JS snapshot with ref tagging"
```

---

## Task 6: Vision client (ollama /api/generate)

**Files:**
- Create: `internal/tools/browser/vision.go`
- Test: `internal/tools/browser/vision_test.go`

- [ ] **Step 1: Write the failing test (httptest stub — no ollama needed)**

```go
// internal/tools/browser/vision_test.go
package browser

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDescribeImage_PostsAndReturnsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/generate") {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		if req["model"] != "llava" {
			t.Errorf("model not sent: %v", req["model"])
		}
		imgs, ok := req["images"].([]any)
		if !ok || len(imgs) != 1 {
			t.Errorf("images not sent: %v", req["images"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "a red login button"})
	}))
	defer srv.Close()

	vc := visionClient{model: "llava", endpoint: srv.URL}
	got, err := vc.describe(context.Background(), []byte{0x89, 0x50}, "what do you see?")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a red login button" {
		t.Errorf("got %q", got)
	}
}

func TestDescribeImage_EndpointDownErrors(t *testing.T) {
	vc := visionClient{model: "llava", endpoint: "http://127.0.0.1:0"}
	if _, err := vc.describe(context.Background(), []byte{1}, "x"); err == nil {
		t.Error("expected error when endpoint unreachable")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/browser/ -run TestDescribeImage -v`
Expected: FAIL — `visionClient` undefined.

- [ ] **Step 3: Implement vision client**

```go
// internal/tools/browser/vision.go
package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const visionTimeout = 60 * time.Second

type visionClient struct {
	model    string
	endpoint string // ollama base url
}

// describe sends a PNG to the ollama generate endpoint and returns the text.
func (vc visionClient) describe(ctx context.Context, png []byte, question string) (string, error) {
	if question == "" {
		question = "Describe this web page screenshot: layout, visible text, and interactive elements."
	}
	body, _ := json.Marshal(map[string]any{
		"model":  vc.model,
		"prompt": question,
		"images": []string{base64.StdEncoding.EncodeToString(png)},
		"stream": false,
	})
	url := strings.TrimRight(vc.endpoint, "/") + "/api/generate"

	cctx, cancel := context.WithTimeout(ctx, visionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision endpoint returned %d", resp.StatusCode)
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("vision decode failed: %w", err)
	}
	return strings.TrimSpace(out.Response), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/browser/ -run TestDescribeImage -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/browser/vision.go internal/tools/browser/vision_test.go
git commit -m "feat(browser): ollama vision client for screenshots"
```

---

## Task 7: The tools (open, snapshot, console, click, type, screenshot)

**Files:**
- Create: `internal/tools/browser/tools.go`
- Test: `internal/tools/browser/tools_test.go`

Each tool wraps the shared `*Session`. All implement `tools.Tool` (`Spec()` + `Run()`). Bad input returns `Result{IsError:true}` (never a Go error), matching the godoc/escalate pattern. `New(cfg)` returns the slice of tools to register; screenshot is included only when a vision model is set.

- [ ] **Step 1: Write the failing test (specs + input validation — no browser)**

```go
// internal/tools/browser/tools_test.go
package browser

import (
	"context"
	"testing"
)

func TestNew_RegistersCoreTools(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true"})
	names := map[string]bool{}
	for _, x := range ts {
		names[x.Spec().Name] = true
	}
	for _, want := range []string{"browser_open", "browser_snapshot", "browser_console", "browser_click", "browser_type"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
	if names["browser_screenshot"] {
		t.Error("screenshot must be absent when no vision model set")
	}
}

func TestNew_AddsScreenshotWhenVisionSet(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true", VisionModel: "llava", VisionEndpoint: "http://x"})
	found := false
	for _, x := range ts {
		if x.Spec().Name == "browser_screenshot" {
			found = true
		}
	}
	if !found {
		t.Error("screenshot tool should be registered when vision model set")
	}
}

func TestOpen_MissingURLIsError(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true"})
	var open Tool
	for _, x := range ts {
		if x.Spec().Name == "browser_open" {
			open = x.(Tool)
		}
	}
	res, err := open.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !res.IsError {
		t.Error("missing url should be IsError result")
	}
}

func TestClick_MissingRefIsError(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true"})
	for _, x := range ts {
		if x.Spec().Name == "browser_click" {
			res, _ := x.Run(context.Background(), map[string]any{})
			if !res.IsError {
				t.Error("missing ref should be IsError")
			}
		}
	}
}
```

Note: `Tool` is exported as the concrete type so the test can pull `browser_open`; alternatively assert via the `tools.Tool` interface. Keep whichever the implementation in Step 3 uses consistent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/browser/ -run 'TestNew_|TestOpen_|TestClick_' -v`
Expected: FAIL — `New`, `Options`, `Tool` undefined.

- [ ] **Step 3: Implement the tools**

```go
// internal/tools/browser/tools.go
package browser

import (
	"context"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// Options configures the tool set.
type Options struct {
	ChromePath     string
	Headless       bool
	VisionModel    string
	VisionEndpoint string
}

// Tool is one browser tool bound to a shared session.
type Tool struct {
	name string
	desc string
	sess *Session
	run  func(ctx context.Context, s *Session, in map[string]any) tools.Result
}

func (t Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: t.name, Description: t.desc, Schema: schemaFor(t.name)}
}

func (t Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	return t.run(ctx, t.sess, in), nil
}

// New builds the browser tool set sharing one lazy session. screenshot is
// included only when VisionModel is set.
func New(opt Options) []tools.Tool {
	sess := NewSession(opt.ChromePath, opt.Headless)
	defs := []Tool{
		{name: "browser_open", desc: "Open a URL in the browser and return the page title plus an accessibility snapshot. Input: {\"url\": string}.", sess: sess, run: runOpen},
		{name: "browser_snapshot", desc: "Return the current page's accessibility snapshot (interactive elements with refs).", sess: sess, run: runSnapshot},
		{name: "browser_console", desc: "Return console messages (logs, warnings, errors) since the last call.", sess: sess, run: runConsole},
		{name: "browser_click", desc: "Click an element by its ref from a snapshot. Input: {\"ref\": \"e5\"}. Returns a fresh snapshot.", sess: sess, run: runClick},
		{name: "browser_type", desc: "Type text into an element by ref. Input: {\"ref\": \"e6\", \"text\": \"hi\"}. Returns a fresh snapshot.", sess: sess, run: runType},
	}
	out := make([]tools.Tool, 0, len(defs)+1)
	for _, d := range defs {
		out = append(out, d)
	}
	if opt.VisionModel != "" {
		vc := visionClient{model: opt.VisionModel, endpoint: opt.VisionEndpoint}
		out = append(out, Tool{
			name: "browser_screenshot",
			desc: "Capture a screenshot and return a text description from a local vision model. Input: {\"question\": string?}.",
			sess: sess,
			run:  screenshotRunner(vc),
		})
	}
	return out
}

func runOpen(ctx context.Context, s *Session, in map[string]any) tools.Result {
	url, _ := in["url"].(string)
	if strings.TrimSpace(url) == "" {
		return tools.Result{Content: "missing or empty 'url'", IsError: true}
	}
	var title string
	if err := s.run(ctx, chromedp.Navigate(url), chromedp.Title(&title)); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}
	}
	snap, err := s.snapshot(ctx)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}
	}
	return tools.Result{Content: fmt.Sprintf("title: %s\n\n%s", title, snap)}
}

func runSnapshot(ctx context.Context, s *Session, _ map[string]any) tools.Result {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}
	}
	return tools.Result{Content: snap}
}

func runConsole(_ context.Context, s *Session, _ map[string]any) tools.Result {
	msgs := s.drainConsole()
	if len(msgs) == 0 {
		return tools.Result{Content: "(no console messages)"}
	}
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s] %s\n", m.Level, m.Text)
	}
	return tools.Result{Content: strings.TrimRight(b.String(), "\n")}
}

func runClick(ctx context.Context, s *Session, in map[string]any) tools.Result {
	ref, _ := in["ref"].(string)
	if strings.TrimSpace(ref) == "" {
		return tools.Result{Content: "missing 'ref' (e.g. \"e5\" from a snapshot)", IsError: true}
	}
	if err := s.run(ctx, chromedp.Click(refSelector(ref), chromedp.ByQuery)); err != nil {
		return tools.Result{Content: fmt.Sprintf("click %s failed: %v", ref, err), IsError: true}
	}
	snap, err := s.snapshot(ctx)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}
	}
	return tools.Result{Content: snap}
}

func runType(ctx context.Context, s *Session, in map[string]any) tools.Result {
	ref, _ := in["ref"].(string)
	text, _ := in["text"].(string)
	if strings.TrimSpace(ref) == "" {
		return tools.Result{Content: "missing 'ref'", IsError: true}
	}
	if err := s.run(ctx, chromedp.SendKeys(refSelector(ref), text, chromedp.ByQuery)); err != nil {
		return tools.Result{Content: fmt.Sprintf("type into %s failed: %v", ref, err), IsError: true}
	}
	snap, err := s.snapshot(ctx)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}
	}
	return tools.Result{Content: snap}
}

func screenshotRunner(vc visionClient) func(context.Context, *Session, map[string]any) tools.Result {
	return func(ctx context.Context, s *Session, in map[string]any) tools.Result {
		question, _ := in["question"].(string)
		var png []byte
		if err := s.run(ctx, chromedp.CaptureScreenshot(&png)); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}
		}
		desc, err := vc.describe(ctx, png, question)
		if err != nil {
			return tools.Result{Content: "vision: " + err.Error(), IsError: true}
		}
		return tools.Result{Content: desc}
	}
}

// Close shuts down the shared session. The first tool in a registry built by
// New carries the session; callers wanting cleanup should keep the *Session.
func schemaFor(name string) map[string]any {
	str := map[string]any{"type": "string"}
	switch name {
	case "browser_open":
		return obj(map[string]any{"url": str}, "url")
	case "browser_click":
		return obj(map[string]any{"ref": str}, "ref")
	case "browser_type":
		return obj(map[string]any{"ref": str, "text": str}, "ref", "text")
	case "browser_screenshot":
		return obj(map[string]any{"question": str})
	default:
		return obj(map[string]any{})
	}
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/browser/ -run 'TestNew_|TestOpen_|TestClick_' -v`
Expected: PASS.

- [ ] **Step 5: Verify build + vet clean**

Run: `go build ./... && go vet ./internal/tools/browser/`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/browser/tools.go internal/tools/browser/tools_test.go
git commit -m "feat(browser): open/snapshot/console/click/type/screenshot tools"
```

---

## Task 8: Register tools + --browser flag + browse subcommand

**Files:**
- Modify: `cmd/bee/run_tools.go` (register browser tools in `buildToolsWithApprover`)
- Modify: `cmd/bee/run.go` (add `--browser` flag that sets `cfg.Browser.Enabled = true`)
- Create: `cmd/bee/browse.go` (`bee browse <url>`)
- Modify: `cmd/bee/main.go` (dispatch `case "browse"`)
- Test: `cmd/bee/browse_test.go`

The session created by `New` is not explicitly closed (headful: the user may keep inspecting; the browser dies with the process). This is acceptable for Phase 1 and documented.

- [ ] **Step 1: Register browser tools**

In `cmd/bee/run_tools.go`, add after `all = appendUserTools(all, cfg.UserTools)` (around line 181):

```go
	all = appendBrowserTools(all, cfg)
```

Then add the helper at the end of the file:

```go
// appendBrowserTools registers the native browser tools when [browser] is
// enabled and a Chrome/Chromium binary is found. Silent no-op otherwise so
// sessions without the opt-in see no extra surface. screenshot is added only
// when [browser.vision] model is set (handled inside browser.New).
func appendBrowserTools(all []tools.Tool, cfg config.Config) []tools.Tool {
	if !cfg.Browser.Enabled {
		return all
	}
	path, err := browser.DetectChrome(cfg.Browser.ChromePath)
	if err != nil {
		return all
	}
	return append(all, browser.New(browser.Options{
		ChromePath:     path,
		Headless:       cfg.Browser.Headless,
		VisionModel:    cfg.Browser.Vision.Model,
		VisionEndpoint: cfg.Browser.Vision.Endpoint,
	})...)
}
```

Add the import `"github.com/elhenro/bee/internal/tools/browser"` to `run_tools.go`.

- [ ] **Step 2: Add --browser flag**

In `cmd/bee/run.go`, alongside the other flags (after line 56):

```go
	browserOn := fs.Bool("browser", false, "enable native browser tools (open/snapshot/click/type/console) for this run")
```

After flags parse and `cfg` is loaded (near where other flag overrides apply, before `buildToolsWithApprover` is called around line 160), add:

```go
	if *browserOn {
		cfg.Browser.Enabled = true
	}
```

- [ ] **Step 3: Write the failing test for browse arg handling**

```go
// cmd/bee/browse_test.go
package main

import "testing"

func TestBrowseArgsToRun_BuildsPrompt(t *testing.T) {
	args, err := browseArgsToRun([]string{"http://localhost:3000"})
	if err != nil {
		t.Fatal(err)
	}
	// must enable browser and carry the url in the seeded prompt
	var sawBrowser bool
	var prompt string
	for i, a := range args {
		if a == "--browser" {
			sawBrowser = true
		}
		if i == len(args)-1 {
			prompt = a
		}
	}
	if !sawBrowser {
		t.Error("browse must pass --browser")
	}
	if !contains(prompt, "http://localhost:3000") {
		t.Errorf("prompt missing url: %q", prompt)
	}
}

func TestBrowseArgsToRun_RequiresURL(t *testing.T) {
	if _, err := browseArgsToRun(nil); err == nil {
		t.Error("expected error when no url given")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (stringIndex(s, sub) >= 0) }
func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./cmd/bee/ -run TestBrowseArgsToRun -v`
Expected: FAIL — `browseArgsToRun` undefined.

- [ ] **Step 5: Implement browse subcommand**

```go
// cmd/bee/browse.go
package main

import (
	"errors"
	"fmt"
	"strings"
)

// browseArgsToRun turns `bee browse <url> [instructions...]` into args for
// the headless run path: enable the browser and seed an open/observe prompt.
func browseArgsToRun(args []string) ([]string, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, errors.New("usage: bee browse <url> [instructions]")
	}
	url := args[0]
	extra := strings.TrimSpace(strings.Join(args[1:], " "))
	prompt := fmt.Sprintf(
		"Open %s with browser_open, read the accessibility snapshot, then call browser_console and report any errors. %s",
		url, extra,
	)
	return []string{"--browser", "--headless=false", prompt}, nil
}

// browse is the `bee browse` subcommand entry point.
func browse(args []string) {
	runArgs, err := browseArgsToRun(args)
	if err != nil {
		fmt.Fprintln(stderr(), err)
		osExit(2)
		return
	}
	runHeadless(runArgs)
}
```

Note: if `stderr()`/`osExit()` helpers don't exist in `cmd/bee`, use `os.Stderr` and `os.Exit` directly (check existing usage in `main.go`). Match whatever pattern the package already uses.

- [ ] **Step 6: Dispatch in main.go**

In `cmd/bee/main.go` switch (after `case "bench":`):

```go
	case "browse":
		browse(os.Args[2:])
```

- [ ] **Step 7: Run tests + build**

Run: `go test ./cmd/bee/ -run TestBrowseArgsToRun -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 8: Commit**

```bash
git add cmd/bee/run_tools.go cmd/bee/run.go cmd/bee/browse.go cmd/bee/browse_test.go cmd/bee/main.go
git commit -m "feat(browser): register tools, --browser flag, bee browse subcommand"
```

---

## Task 9: Env-gated integration test (real Chrome)

**Files:**
- Create: `internal/tools/browser/integration_test.go`

Exercises the real loop: launch Chrome, open a `data:` URL with a button and a `console.log`, snapshot finds the button ref, click mutates the DOM, console drains the log. Skipped unless `BEE_BROWSER_TEST=1` so CI without Chrome passes.

- [ ] **Step 1: Write the integration test**

```go
// internal/tools/browser/integration_test.go
package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegration_OpenSnapshotClickConsole(t *testing.T) {
	if os.Getenv("BEE_BROWSER_TEST") != "1" {
		t.Skip("set BEE_BROWSER_TEST=1 to run (needs Chrome/Chromium)")
	}
	path, err := DetectChrome("")
	if err != nil {
		t.Skipf("no chrome: %v", err)
	}
	sess := NewSession(path, true) // headless for CI
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const page = `data:text/html,<html><body>
<button onclick="console.log('clicked!');document.title='done'">Press</button>
<script>console.log('loaded')</script></body></html>`

	ts := New(Options{ChromePath: path, Headless: true})
	var open, snap, console, click Tool
	for _, x := range ts {
		switch x.Spec().Name {
		case "browser_open":
			open = x.(Tool)
		case "browser_snapshot":
			snap = x.(Tool)
		case "browser_console":
			console = x.(Tool)
		case "browser_click":
			click = x.(Tool)
		}
	}
	// share the same session across these tool instances
	open.sess, snap.sess, console.sess, click.sess = sess, sess, sess, sess

	if r := open.Run2(ctx, map[string]any{"url": page}); r.IsError {
		t.Fatalf("open: %s", r.Content)
	}
	s := snap.Run2(ctx, nil)
	if !strings.Contains(s.Content, "button") || !strings.Contains(s.Content, "[e") {
		t.Fatalf("snapshot missing button/ref: %s", s.Content)
	}
	// extract first ref
	ref := firstRef(s.Content)
	if ref == "" {
		t.Fatal("no ref parsed")
	}
	if r := click.Run2(ctx, map[string]any{"ref": ref}); r.IsError {
		t.Fatalf("click: %s", r.Content)
	}
	c := console.Run2(ctx, nil)
	if !strings.Contains(c.Content, "clicked!") && !strings.Contains(c.Content, "loaded") {
		t.Fatalf("console missing logs: %s", c.Content)
	}
}

func firstRef(snap string) string {
	i := strings.Index(snap, "[e")
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(snap[i:], ']')
	if j < 0 {
		return ""
	}
	return snap[i+1 : i+j]
}
```

Note: the test uses a `Run2` helper that returns `tools.Result` directly (no error) for terser assertions. Add it to `tools.go`:

```go
// Run2 is a test/internal convenience that drops the always-nil error.
func (t Tool) Run2(ctx context.Context, in map[string]any) tools.Result {
	r, _ := t.Run(ctx, in)
	return r
}
```

(If you prefer not to add internal helpers, call `Run` and discard the error in the test instead — pick one and keep it consistent.)

- [ ] **Step 2: Run it locally with Chrome**

Run: `BEE_BROWSER_TEST=1 go test ./internal/tools/browser/ -run TestIntegration -v`
Expected: PASS (launches headless Chrome, exercises the full loop).

- [ ] **Step 3: Confirm it skips without the env var**

Run: `go test ./internal/tools/browser/ -run TestIntegration -v`
Expected: SKIP.

- [ ] **Step 4: Commit**

```bash
git add internal/tools/browser/integration_test.go internal/tools/browser/tools.go
git commit -m "test(browser): env-gated end-to-end Chrome integration test"
```

---

## Task 10: Bundled browse recipe skill

**Files:**
- Create: `internal/skills/bundled/browse.md`
- Test: confirm it parses (existing bundled-skill tests cover the dir, or add a focused parse test)

- [ ] **Step 1: Write the recipe skill**

```markdown
---
name: browse
description: Open a URL, snapshot the page, and report console errors. Use to check a running website or game.
kind: recipe
steps:
  - id: open
    description: open the target URL and capture the accessibility snapshot
    tool: browser_open
  - id: console
    description: read console output and surface any errors or warnings
    tool: browser_console
---

Open the page the user named, read the snapshot to understand the layout and
interactive elements (each has a `[ref]`), then check the console for errors.
Report what you see and what (if anything) is broken. To interact, call
`browser_click` / `browser_type` with a ref from the snapshot.
```

- [ ] **Step 2: Verify it parses**

Run: `go test ./internal/skills/ -v` (bundled skills are parsed by existing tests)
Expected: PASS — `browse` parses as a recipe; no "missing description"/"no steps" errors.

If no existing test loads `bundled/`, add:

```go
// internal/skills/bundled_browse_test.go
package skills

import (
	"os"
	"testing"
)

func TestBundledBrowseParses(t *testing.T) {
	b, err := os.ReadFile("bundled/browse.md")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseBytes(b, "bundled/browse.md") // use whatever the package's parse entrypoint is
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Kind != KindRecipe {
		t.Errorf("kind = %q", s.Kind)
	}
}
```

Adjust `ParseBytes` to the actual exported parse function in `parse.go` (check its signature first).

- [ ] **Step 3: Commit**

```bash
git add internal/skills/bundled/browse.md internal/skills/bundled_browse_test.go
git commit -m "feat(browser): bundled browse recipe skill"
```

---

## Task 11: Docs + final verification

**Files:**
- Modify: `AGENTS.md` and/or `README.md` — document the `[browser]` config, `--browser` flag, `bee browse`, and vision setup.

- [ ] **Step 1: Document the feature**

Add a short "Browser tools" section to `README.md` covering:
- enable via `[browser] enabled = true` or `--browser` / `bee browse <url>`
- headful by default; `headless = true` for CI
- requires an installed Chrome/Chromium (auto-detected; override with `chrome_path`)
- optional `[browser.vision]` for screenshot-to-text via local ollama
- the tool list: `browser_open`, `browser_snapshot`, `browser_console`, `browser_click`, `browser_type`, `browser_screenshot`

Keep prose plain — no em-dashes, commas or split sentences instead.

- [ ] **Step 2: Full build + vet + test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all clean/pass (integration test skips without `BEE_BROWSER_TEST`).

- [ ] **Step 3: golangci-lint**

Run: `golangci-lint run`
Expected: clean.

- [ ] **Step 4: Smoke the subcommand help path**

Run: `go build -o /tmp/bee ./cmd/bee && /tmp/bee browse` (no url)
Expected: prints `usage: bee browse <url> [instructions]` and exits non-zero.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs(browser): document browser tools, --browser, bee browse"
```

---

## Self-Review Notes

- **Spec coverage:** all 6 tools (Task 7), headful default (Tasks 1/4/8), opt-in via config+flag (Tasks 1/8), auto-detect Chrome (Task 2), vision via dedicated config block (Tasks 1/6/7), browse subcommand + recipe (Tasks 8/10), error handling (Result.IsError throughout), testing strategy incl. env-gated integration (Task 9). Covered.
- **Snapshot deviation:** plan uses injected-JS `data-bee-ref` tagging instead of `getFullAXTree`; same `- role "name" [ref]` contract, simpler click/type. Documented at top.
- **Type consistency:** `Tool`, `Options`, `New`, `Session`, `NewSession`, `visionClient.describe`, `refSelector`, `snapshotJS`, `consoleRingMax` used consistently across tasks. `Run2` helper introduced in Task 7/9 for terse tests (optional).
- **Open verification points for the implementer:** confirm `cmd/bee` stderr/exit pattern (Task 8 Step 5), confirm skills parse entrypoint signature (Task 10 Step 2), confirm `chromedp.DefaultExecAllocatorOptions` slice-copy idiom builds on the pinned chromedp version (Task 4).
```
