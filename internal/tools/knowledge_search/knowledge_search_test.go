package knowledge_search

import (
	"context"
	"strings"
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
