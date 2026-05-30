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

// Run2 drops the always-nil error from Run for terser internal/test callers.
func (t Tool) Run2(ctx context.Context, in map[string]any) tools.Result {
	r, _ := t.Run(ctx, in)
	return r
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
