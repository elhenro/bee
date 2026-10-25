package loop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

func TestAppendPromptTail_KeepsDynamicASuffix(t *testing.T) {
	cases := []struct{ sys, dyn, tail string }{
		{"static\n\nmemory", "memory", "card"},
		{"static only", "", "card"},
		{"", "", "card"},
		{"static\n\nmemory", "memory", ""},
	}
	for _, c := range cases {
		sys, dyn := appendPromptTail(c.sys, c.dyn, c.tail)
		if c.tail == "" {
			if sys != c.sys || dyn != c.dyn {
				t.Errorf("empty tail changed inputs: %q %q", sys, dyn)
			}
			continue
		}
		if !strings.HasSuffix(sys, c.tail) || !strings.HasSuffix(dyn, c.tail) {
			t.Errorf("tail not appended: sys=%q dyn=%q", sys, dyn)
		}
		if dyn != "" && !strings.HasSuffix(sys, dyn) {
			t.Errorf("dynamic %q is not a suffix of %q, cache split breaks", dyn, sys)
		}
		if c.dyn != "" && !strings.Contains(dyn, c.dyn) {
			t.Errorf("memory tail dropped from dynamic: %q", dyn)
		}
		if c.sys != "" && !strings.HasPrefix(sys, c.sys) {
			t.Errorf("static prefix changed: %q", sys)
		}
	}
}

// sysCaptureProvider records the system prompt of every call.
type sysCaptureProvider struct {
	mu   sync.Mutex
	seen []llm.Request
}

func (p *sysCaptureProvider) Name() string { return "capture" }
func (p *sysCaptureProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	p.seen = append(p.seen, req)
	p.mu.Unlock()
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: "ok"}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func TestRun_PromptTailRidesTheDynamicSuffix(t *testing.T) {
	p := &sysCaptureProvider{}
	cfg := config.Defaults()
	cfg.Caveman = "off"
	cfg.Role = "worker"
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	eng := &Engine{Provider: p, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: ".", Stdout: &strings.Builder{}}
	eng.SetPromptTail(func() string { return "[pheromone] CARD-MARKER" })
	if _, err := eng.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.seen) != 1 {
		t.Fatalf("calls = %d", len(p.seen))
	}
	req := p.seen[0]
	if !strings.HasSuffix(req.System, "CARD-MARKER") {
		t.Errorf("system prompt does not end with the tail:\n%s", req.System)
	}
	if !strings.HasSuffix(req.SystemDynamic, "CARD-MARKER") || !strings.HasSuffix(req.System, req.SystemDynamic) {
		t.Errorf("tail must sit in the dynamic suffix: dyn=%q", req.SystemDynamic)
	}
	// nil hook: byte-identical prompt, no stray separators.
	eng.PromptTail = nil
	if _, err := eng.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(p.seen[1].System, "CARD-MARKER") {
		t.Error("tail leaked into a run without the hook")
	}
}

func TestBuildDeathReport_ContractShape(t *testing.T) {
	p := &compactStubProvider{summary: "tried A then B"}
	rep, err := BuildDeathReport(context.Background(), p, "stub", longHistory(), errors.New("max iterations (3) reached"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## report", "status: died", "cause: max iterations (3) reached", "did: tried A then B", "next:", "TAIL-ASSISTANT-LINE"} {
		if !strings.Contains(rep, want) {
			t.Errorf("death report missing %q:\n%s", want, rep)
		}
	}
	if strings.Contains(rep, "stronger model") {
		t.Error("death report carries the rescue-handoff framing")
	}
	// short run: no middle to summarize, provider never called.
	calls := 0
	cp := &countingProvider{onStream: func() { calls++ }}
	rep, err = BuildDeathReport(context.Background(), cp, "stub", longHistory()[:2], nil)
	if err != nil || calls != 0 {
		t.Fatalf("short run: err=%v calls=%d", err, calls)
	}
	if !strings.Contains(rep, "nothing recorded") || strings.Contains(rep, "cause:") {
		t.Errorf("short run report:\n%s", rep)
	}
}
