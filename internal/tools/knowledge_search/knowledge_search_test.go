package knowledge_search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

type stubProvider struct {
	resp string
}

func (s stubProvider) Name() string { return "stub" }

func (s stubProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: s.resp}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

func TestSpec(t *testing.T) {
	tool := New(nil, "", "", 3)
	spec := tool.Spec()
	if spec.Name != "knowledge_search" {
		t.Fatalf("name %q, want knowledge_search", spec.Name)
	}
}

func TestRunMissingQuery(t *testing.T) {
	tool := New(nil, "", "", 3)
	res, _ := tool.Run(context.Background(), map[string]any{})
	if !res.IsError {
		t.Fatal("expected error for missing query")
	}
	if !strings.Contains(res.Content, "missing") {
		t.Fatalf("unexpected content: %q", res.Content)
	}
}

func TestRunMissingDir(t *testing.T) {
	tool := New(nil, "", "", 3)
	res, _ := tool.Run(context.Background(), map[string]any{"query": "test"})
	if !res.IsError {
		t.Fatal("expected error for missing dir")
	}
}

func TestRunReturnsRecords(t *testing.T) {
	dir := "../../knowledge/testdata/store"
	tool := New(stubProvider{resp: "testing\n"}, "m", dir, 3)
	res, err := tool.Run(context.Background(), map[string]any{"query": "testing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "#") {
		t.Fatalf("expected formatted header in result, got:\n%s", res.Content)
	}
}

func TestRunNoMatches(t *testing.T) {
	// stub returns no tags so phase-2 also produces nothing.
	tool := New(stubProvider{resp: ""}, "m", "../../knowledge/testdata/empty", 3)
	res, err := tool.Run(context.Background(), map[string]any{"query": "nonsense"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Content)
	}
	if !strings.Contains(res.Content, "no matching") {
		t.Fatalf("expected 'no matching' message, got: %q", res.Content)
	}
}

func TestToolInterface(t *testing.T) {
	var _ tools.Tool = (*Tool)(nil)
}

// countingProvider tracks how many times Stream was invoked so tests can
// assert phase-2 fired or didn't.
type countingProvider struct {
	resp  string
	calls atomic.Int32
}

func (c *countingProvider) Name() string { return "counting" }

func (c *countingProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	c.calls.Add(1)
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: c.resp}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

// regression: phase-2 must fire when phase-1 returns fewer than topK
// records, not only when results are below the old hard-coded 2.
func TestPhase2FiresWhenBelowTopK(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: only\ndescription: solitary record\ntags: [misc]\npriority: 3\nexpires: never\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "only.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// phase-1 yields 1 record (matches name token "only"); topK=3 means
	// phase-2 must still fire to look for more.
	prov := &countingProvider{resp: "misc\n"}
	tool := New(prov, "m", dir, 3)
	if _, err := tool.Run(context.Background(), map[string]any{"query": "only"}); err != nil {
		t.Fatal(err)
	}
	if prov.calls.Load() == 0 {
		t.Fatal("phase-2 did not fire when results below topK")
	}
}

// regression: phase-2 must NOT fire when phase-1 already filled the top-K.
func TestPhase2SkipsWhenAtTopK(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"alpha", "beta", "gamma"} {
		body := "---\nname: " + n + "\ndescription: query me\ntags: [misc]\npriority: 3\nexpires: never\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, n+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prov := &countingProvider{resp: "misc\n"}
	tool := New(prov, "m", dir, 3)
	if _, err := tool.Run(context.Background(), map[string]any{"query": "query"}); err != nil {
		t.Fatal(err)
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("phase-2 fired when top-K already filled: %d calls", prov.calls.Load())
	}
}

func TestSpecSchema(t *testing.T) {
	tool := New(nil, "", "", 3)
	spec := tool.Spec()
	props, ok := spec.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties map")
	}
	if _, ok := props["query"]; !ok {
		t.Fatal("missing query property")
	}
}
