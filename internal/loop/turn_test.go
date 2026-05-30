package loop

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// stubProvider scripts a sequence of event batches — one batch per call.
type stubProvider struct {
	scripts [][]llm.Event
	calls   atomic.Int32
	// hold blocks the second message of each batch until ctx is canceled,
	// for the cancellation test.
	hold bool
}

func (p *stubProvider) Name() string { return "stub" }

func (p *stubProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	idx := int(p.calls.Add(1)) - 1
	if idx >= len(p.scripts) {
		// keep emitting tool_use to trigger the cap test
		idx = len(p.scripts) - 1
	}
	batch := p.scripts[idx]
	ch := make(chan llm.Event, len(batch)+1)
	go func() {
		defer close(ch)
		for i, ev := range batch {
			if p.hold && i == 1 {
				select {
				case <-ctx.Done():
					ch <- llm.Event{Type: llm.EventError, Err: ctx.Err()}
					return
				case <-time.After(time.Second):
				}
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

// stubTool runs a function on input.
type stubTool struct {
	name string
	desc string
	fn   func(ctx context.Context, in map[string]any) (tools.Result, error)
}

func (s *stubTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: s.name, Description: s.desc}
}
func (s *stubTool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	return s.fn(ctx, in)
}

type stubMemStore struct{}

func (stubMemStore) Query(_ context.Context, _ string, _ []string) ([]knowledge.Record, error) {
	return nil, nil
}

func newEngine(p llm.Provider, reg *tools.Registry) (*Engine, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	cfg := config.Defaults()
	cfg.Caveman = "off"
	// force edit mode in tests so the classifier doesn't burn a stub script.
	cfg.Mode = "edit"
	// pin profile so tests don't pick up tiny's MaxIterations=12 from the
	// auto-resolution against deepseek-flash.
	cfg.Profile = "normal"
	// disable sandbox wrapping in tests so shell tool input stays inspectable
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	return &Engine{
		Provider: p,
		Tools:    reg,
		Memory:   stubMemStore{},
		Cfg:      cfg,
		Cwd:      ".",
		Stdout:   buf,
	}, buf
}

func TestFilterToolSpecsForProfile(t *testing.T) {
	specs := []llm.ToolSpec{
		{Name: "bash"}, {Name: "read"}, {Name: "write"}, {Name: "edit"},
		{Name: "search"}, {Name: "glob"}, {Name: "ls"},
		{Name: "apply_patch"}, {Name: "hashline_edit"}, {Name: "knowledge_search"},
	}
	// tiny: 4-tool minimum {bash, read, write, edit}
	got := filterToolSpecsForProfile(specs, "tiny")
	if len(got) != 4 {
		t.Fatalf("tiny profile should keep 4 tools, got %d: %+v", len(got), got)
	}
	keep := map[string]bool{}
	for _, s := range got {
		keep[s.Name] = true
	}
	for _, want := range []string{"bash", "read", "write", "edit"} {
		if !keep[want] {
			t.Errorf("tiny profile missing %s", want)
		}
	}
	// normal: 7-tool surface + knowledge_search.
	// Drops apply_patch + hashline_edit (large-profile extras).
	normal := filterToolSpecsForProfile(specs, "normal")
	keepNormal := map[string]bool{}
	for _, s := range normal {
		keepNormal[s.Name] = true
	}
	for _, want := range []string{"bash", "read", "write", "edit", "search", "glob", "ls", "knowledge_search"} {
		if !keepNormal[want] {
			t.Errorf("normal profile missing %s", want)
		}
	}
	for _, drop := range []string{"apply_patch", "hashline_edit"} {
		if keepNormal[drop] {
			t.Errorf("normal profile should drop %s (large-only)", drop)
		}
	}
	// large (no allowlist) passes through unfiltered.
	large := filterToolSpecsForProfile(specs, "large")
	if len(large) != len(specs) {
		t.Errorf("large profile should pass through, got %d/%d", len(large), len(specs))
	}
}

// ExtraTools is the opt-in hatch: small profile + named extras = the expert
// tools surface without paying for the large profile's prompt budget.
func TestFilterToolSpecsForProfile_ExtrasUnionWithAllowlist(t *testing.T) {
	specs := []llm.ToolSpec{
		{Name: "bash"}, {Name: "read"}, {Name: "write"}, {Name: "edit"},
		{Name: "apply_patch"}, {Name: "hashline_edit"},
	}
	got := filterToolSpecsForProfile(specs, "tiny", "apply_patch")
	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	if !names["apply_patch"] {
		t.Errorf("extras should force apply_patch into tiny manifest")
	}
	if names["hashline_edit"] {
		t.Errorf("only listed extras should pass; hashline_edit leaked")
	}
	// baseline tiny tools must still be present.
	for _, want := range []string{"bash", "read", "write", "edit"} {
		if !names[want] {
			t.Errorf("tiny baseline %q dropped when extras supplied", want)
		}
	}
}

// Disabled tools are dropped from the manifest regardless of profile.
func TestFilterToolSpecsDisabled(t *testing.T) {
	specs := []llm.ToolSpec{{Name: "bash"}, {Name: "read"}, {Name: "write"}}
	got := filterToolSpecsDisabled(specs, []string{"write"})
	if len(got) != 2 {
		t.Fatalf("want 2 after dropping write, got %d", len(got))
	}
	for _, s := range got {
		if s.Name == "write" {
			t.Error("write should have been removed")
		}
	}
}

// Empty disabled list passes through unchanged.
func TestFilterToolSpecsDisabled_EmptyPassthrough(t *testing.T) {
	specs := []llm.ToolSpec{{Name: "bash"}}
	got := filterToolSpecsDisabled(specs, nil)
	if &got[0] != &specs[0] && len(got) != 1 {
		t.Error("empty disabled should pass through")
	}
}

// Canonical bee tool names: a stable 7-tool surface for the normal profile.
func TestCanonicalToolNames(t *testing.T) {
	// canonical set: bash, edit, glob, search, ls, read, write. normal profile
	// also exposes knowledge_search as a bee extension.
	want := map[string]bool{
		"bash": true, "read": true, "write": true, "edit": true,
		"search": true, "glob": true, "ls": true,
	}
	for name := range want {
		if !profileToolAllowlist["normal"][name] {
			t.Errorf("normal profile missing canonical tool %q", name)
		}
	}
}

func TestRunPureTextResponse(t *testing.T) {
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventTextDelta, Delta: "hello "},
			{Type: llm.EventTextDelta, Delta: "world"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, buf := newEngine(p, tools.NewRegistry())
	res, err := eng.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FinalText != "hello world" {
		t.Errorf("FinalText = %q want %q", res.FinalText, "hello world")
	}
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("stdout missing streamed text: %q", buf.String())
	}
	if len(res.Messages) != 2 {
		t.Errorf("want 2 messages (user+assistant), got %d", len(res.Messages))
	}
}

func TestRunSingleToolUse(t *testing.T) {
	reg := tools.NewRegistry()
	called := 0
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "exec",
		fn: func(_ context.Context, in map[string]any) (tools.Result, error) {
			called++
			return tools.Result{Content: "ok"}, nil
		},
	})

	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
				ID: "u1", Name: "bash", Input: map[string]any{"command": "echo hi"},
			}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "run echo")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called != 1 {
		t.Errorf("tool call count = %d want 1", called)
	}
	if res.FinalText != "done" {
		t.Errorf("FinalText = %q want done", res.FinalText)
	}
}

func TestDispatchToolsRunsReadsInParallel(t *testing.T) {
	// barrier: both tool runs must reach Wait before either returns.
	// if dispatch is serial, second never starts -> Wait blocks -> ctx timeout.
	reg := tools.NewRegistry()
	var wg sync.WaitGroup
	wg.Add(2)
	_ = reg.Register(&stubTool{
		name: "read",
		desc: "read file",
		fn: func(ctx context.Context, _ map[string]any) (tools.Result, error) {
			wg.Done()
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
				return tools.Result{Content: "ok"}, nil
			case <-ctx.Done():
				return tools.Result{Content: "timeout", IsError: true}, ctx.Err()
			}
		},
	})

	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
				ID: "u1", Name: "read", Input: map[string]any{"path": "/a"},
			}},
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
				ID: "u2", Name: "read", Input: map[string]any{"path": "/b"},
			}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := eng.Run(ctx, "two reads")
	if err != nil {
		t.Fatalf("Run failed (likely serial dispatch deadlock): %v", err)
	}
	if res.FinalText != "done" {
		t.Errorf("FinalText = %q want done", res.FinalText)
	}
}

func TestRunTwoSequentialToolUses(t *testing.T) {
	reg := tools.NewRegistry()
	var calls []string
	mkTool := func(name string) tools.Tool {
		return &stubTool{name: name, desc: "x", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			calls = append(calls, name)
			return tools.Result{Content: "ok " + name}, nil
		}}
	}
	_ = reg.Register(mkTool("read"))
	_ = reg.Register(mkTool("bash"))

	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u1", Name: "read", Input: map[string]any{"path": "."}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u2", Name: "bash", Input: map[string]any{"command": "ls"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "both done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "x")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 2 || calls[0] != "read" || calls[1] != "bash" {
		t.Errorf("tool call seq = %v want [view shell]", calls)
	}
	if res.FinalText != "both done" {
		t.Errorf("FinalText = %q", res.FinalText)
	}
}

func TestRunMaxIterationsCap(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	})
	// provider always emits a tool_use — exhausts the cap.
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
	}}
	eng, _ := newEngine(p, reg)
	_, err := eng.Run(context.Background(), "loop me")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Errorf("expected max-iterations error, got %v", err)
	}
	if got := p.calls.Load(); got != int32(MaxIterations) {
		t.Errorf("provider calls = %d want %d", got, MaxIterations)
	}
}

func TestRunMaxIterationsUnlimited(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	})
	// 60 tool-use rounds (past the old 50 cap) then a final text answer.
	scripts := make([][]llm.Event, 0, 61)
	for i := 0; i < 60; i++ {
		scripts = append(scripts, []llm.Event{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		})
	}
	scripts = append(scripts, []llm.Event{
		{Type: llm.EventTextDelta, Delta: "done"},
		{Type: llm.EventDone, StopReason: "stop"},
	})
	p := &stubProvider{scripts: scripts}
	eng, _ := newEngine(p, reg)
	eng.Cfg.MaxIterations = 0 // unlimited: no ceiling, run until the model stops
	res, err := eng.Run(context.Background(), "loop past fifty")
	if err != nil {
		t.Fatalf("unlimited run errored: %v", err)
	}
	if res.FinalText != "done" {
		t.Errorf("FinalText = %q want %q", res.FinalText, "done")
	}
	if got := p.calls.Load(); got != 61 {
		t.Errorf("provider calls = %d, want 61 (ran past the 50 cap)", got)
	}
}

func TestRunOne_UnknownToolListsAvailable(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "bash", desc: "x", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
		return tools.Result{Content: ""}, nil
	}})
	_ = reg.Register(&stubTool{name: "read", desc: "x", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
		return tools.Result{Content: ""}, nil
	}})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u1", Name: `bogus<｜DSML｜x`, Input: map[string]any{}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "ok"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for _, m := range res.Messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockToolResult && c.Result != nil {
				content = c.Result.Content
			}
		}
	}
	if !strings.Contains(content, "unknown tool") {
		t.Fatalf("missing prefix: %q", content)
	}
	if !strings.Contains(content, "available: bash, read") {
		t.Errorf("expected sorted available list, got: %q", content)
	}
	if !strings.Contains(content, "chat-template markup") {
		t.Errorf("expected markup-leak hint, got: %q", content)
	}
}

func TestRunOne_ParseErrorSurfacesDiagnostic(t *testing.T) {
	reg := tools.NewRegistry()
	called := false
	_ = reg.Register(&stubTool{name: "bash", desc: "x", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
		called = true
		return tools.Result{Content: ""}, nil
	}})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
				ID:    "u1",
				Name:  "bash",
				Input: map[string]any{"_parse_error": "decode tool args: bad json", "_raw_args": `{"cmd":` + "\n"},
			}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "ok"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("bash should not have been invoked on parse-error input")
	}
	var content string
	for _, m := range res.Messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockToolResult && c.Result != nil {
				content = c.Result.Content
			}
		}
	}
	if !strings.Contains(content, "failed to parse") {
		t.Errorf("expected parse-error diagnostic, got: %q", content)
	}
	if !strings.Contains(content, "raw=") {
		t.Errorf("expected raw= excerpt, got: %q", content)
	}
}

func TestRunOne_TruncatesShellOutput(t *testing.T) {
	reg := tools.NewRegistry()
	// emit > 50K tokens worth of output (chars/4 heuristic → > 200K chars).
	huge := strings.Repeat("x\n", 150_000)
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: huge}, nil
		},
	})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u1", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "ok"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// find the tool result message
	var toolContent string
	for _, m := range res.Messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockToolResult && c.Result != nil {
				toolContent = c.Result.Content
			}
		}
	}
	if toolContent == "" {
		t.Fatal("no tool result content found")
	}
	if len(toolContent) >= len(huge) {
		t.Errorf("expected truncation: got %d chars, raw was %d", len(toolContent), len(huge))
	}
	if !strings.Contains(toolContent, "truncated") {
		t.Errorf("expected trailer marker; got: %q", toolContent[max(0, len(toolContent)-200):])
	}
}

func TestDetectDoneSignal(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"work done <promise>DONE</promise>", true},
		{"<promise>done</promise>", true},
		{"<PROMISE>DONE</PROMISE>", true},
		{"", false},
		{"not done", false},
		{"<promise>working</promise>", false},
	}
	for _, tc := range cases {
		if got := detectDoneSignal(tc.in); got != tc.want {
			t.Errorf("detectDoneSignal(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRunContextCancellation(t *testing.T) {
	p := &stubProvider{
		hold: true,
		scripts: [][]llm.Event{
			{
				{Type: llm.EventTextDelta, Delta: "before-cancel"},
				{Type: llm.EventTextDelta, Delta: "after-cancel"},
				{Type: llm.EventDone, StopReason: "stop"},
			},
		},
	}
	eng, _ := newEngine(p, tools.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	// 100ms gives the stream goroutine time to reach its hold check even on
	// slow runners (windows scheduler tick ~16ms) — the prior 20ms raced.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := eng.Run(ctx, "x")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// recordingProvider counts Stream calls so we can assert classifier behavior.
type recordingProvider struct {
	calls atomic.Int32
}

func (r *recordingProvider) Name() string { return "recording" }

func (r *recordingProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	r.calls.Add(1)
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: "ok"}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func TestRun_ClassifierCalled_HostedAutoMode(t *testing.T) {
	p := &recordingProvider{}
	buf := &bytes.Buffer{}
	cfg := config.Defaults()
	cfg.Caveman = "off"
	cfg.Mode = "auto"
	cfg.Profile = "normal"
	cfg.DefaultProvider = "openrouter"
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	eng := &Engine{
		Provider: p,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Cfg:      cfg,
		Cwd:      ".",
		Stdout:   buf,
	}
	if _, err := eng.Run(context.Background(), "explain x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 1 classifier + 1 main turn = 2 stream calls
	if got := p.calls.Load(); got != 2 {
		t.Errorf("hosted+auto: provider calls = %d, want 2 (classifier + main)", got)
	}
}

func TestRun_ClassifierSkipped_LocalAutoMode(t *testing.T) {
	p := &recordingProvider{}
	buf := &bytes.Buffer{}
	cfg := config.Defaults()
	cfg.Caveman = "off"
	cfg.Mode = "auto"
	cfg.Profile = "normal"
	cfg.DefaultProvider = "ollama"
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	eng := &Engine{
		Provider: p,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Cfg:      cfg,
		Cwd:      ".",
		Stdout:   buf,
	}
	if _, err := eng.Run(context.Background(), "explain x"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// local provider skips classifier: only the main turn streams
	if got := p.calls.Load(); got != 1 {
		t.Errorf("local+auto: provider calls = %d, want 1 (main only, no classifier)", got)
	}
}

func TestRun_MaxIter_TinyProfileOverrides(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
	}}
	eng, _ := newEngine(p, reg)
	// override test default of normal: tiny → MaxIterations=50 from profile.
	eng.Cfg.Profile = "tiny"
	eng.Cfg.MaxIterations = 0 // clear cfg default so profile wins cleanly
	_, err := eng.Run(context.Background(), "loop me")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("expected max-iterations error, got %v", err)
	}
	if got := p.calls.Load(); got != 50 {
		t.Errorf("tiny profile maxIter: provider calls = %d, want 50", got)
	}
}

func TestRun_MaxIter_NormalProfileFallsThrough(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
	}}
	eng, _ := newEngine(p, reg)
	// profile=normal pinned by newEngine; cfg.MaxIterations=50 from Defaults.
	_, err := eng.Run(context.Background(), "loop me")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("expected max-iterations error, got %v", err)
	}
	if got := p.calls.Load(); got != 50 {
		t.Errorf("normal profile maxIter: provider calls = %d, want 50 (cfg default)", got)
	}
}

// TestRun_TokenBudgetCap drives the adaptive token cap: provider reports
// 200k input tokens per call against a 65k-window model (deepseek-reasoner
// → 650k budget). Loop must bail on token budget BEFORE the 50-iter cap.
func TestRun_TokenBudgetCap(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use", Usage: &llm.Usage{InputTokens: 200000, OutputTokens: 50000}},
		},
	}}
	eng, _ := newEngine(p, reg)
	// 65k-window model → tokenBudget = 650k. Provider emits 250k per call
	// → trips after 3 iters, well under the 50-iter cap.
	eng.Cfg.DefaultModel = "deepseek-reasoner"
	_, err := eng.Run(context.Background(), "loop me")
	if err == nil || !strings.Contains(err.Error(), "token budget") {
		t.Fatalf("expected token-budget error, got %v", err)
	}
	if got := p.calls.Load(); got >= 50 {
		t.Errorf("token cap should fire before iter cap; got %d calls", got)
	}
}

// TestRun_TokenBudgetAutoRecovery asserts the token cap compacts and continues
// up to maxBudgetRecoveries times before hard-stopping, instead of stopping on
// the first hit. The hard-stop error names the recovery count.
func TestRun_TokenBudgetAutoRecovery(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash", desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use", Usage: &llm.Usage{InputTokens: 200000, OutputTokens: 50000}},
		},
	}}
	eng, _ := newEngine(p, reg)
	eng.Cfg.DefaultModel = "deepseek-reasoner" // 650k budget, 250k/call
	_, err := eng.Run(context.Background(), "loop me")
	if err == nil || !strings.Contains(err.Error(), "auto-compactions") {
		t.Fatalf("expected hard stop naming auto-compactions, got %v", err)
	}
	// recovery means it didn't stop at the first trip (~3 iters); each recovery
	// re-arms for another ~3, so total calls exceed a single segment.
	if got := p.calls.Load(); got <= 3 {
		t.Errorf("expected recovery to continue past first trip; got %d calls", got)
	}
	if eng.budgetRecoveries != maxBudgetRecoveries {
		t.Errorf("budgetRecoveries = %d, want %d", eng.budgetRecoveries, maxBudgetRecoveries)
	}
}

// TestRun_StallCap drives the read-only stall cap: provider keeps calling
// `read` (non-mutator) so noMutationStreak grows. Loop must bail before
// the iter cap.
func TestRun_StallCap(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "read",
		desc: "x",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: "ok"}, nil
		},
	})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "read", Input: map[string]any{"path": "f"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
	}}
	eng, _ := newEngine(p, reg)
	_, err := eng.Run(context.Background(), "loop me")
	if err == nil || !strings.Contains(err.Error(), "read-only iters") {
		t.Fatalf("expected stall-cap error, got %v", err)
	}
	if got := p.calls.Load(); got >= 50 {
		t.Errorf("stall cap should fire before iter cap; got %d calls", got)
	}
}
