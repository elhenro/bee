package tool_lookup

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
)

type stubReg struct {
	specs []llm.ToolSpec
}

func (s *stubReg) Specs() []llm.ToolSpec { return s.specs }
func (s *stubReg) Names() []string {
	out := make([]string, 0, len(s.specs))
	for _, sp := range s.specs {
		out = append(out, sp.Name)
	}
	return out
}

func TestLookup_ListsAllNames(t *testing.T) {
	reg := &stubReg{specs: []llm.ToolSpec{
		{Name: "read"},
		{Name: "write"},
		{Name: "shell"},
	}}
	tool := New(reg)
	got, err := tool.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected error: %s", got.Content)
	}
	if got.Content != "read\nshell\nwrite" {
		t.Fatalf("names not sorted/joined: %q", got.Content)
	}
}

func TestLookup_ReturnsSpec(t *testing.T) {
	reg := &stubReg{specs: []llm.ToolSpec{
		{
			Name:        "read",
			Description: "read a file",
			Schema:      map[string]any{"type": "object"},
		},
	}}
	tool := New(reg)
	got, err := tool.Run(context.Background(), map[string]any{"name": "read"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.IsError {
		t.Fatalf("error: %s", got.Content)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got.Content), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["name"] != "read" || parsed["description"] != "read a file" {
		t.Fatalf("unexpected payload: %v", parsed)
	}
	if _, ok := parsed["schema"].(map[string]any); !ok {
		t.Fatalf("schema missing: %v", parsed)
	}
}

func TestLookup_UnknownTool(t *testing.T) {
	reg := &stubReg{specs: []llm.ToolSpec{{Name: "read"}}}
	tool := New(reg)
	got, _ := tool.Run(context.Background(), map[string]any{"name": "nope"})
	if !got.IsError {
		t.Fatalf("expected error result")
	}
	if !strings.Contains(got.Content, "unknown tool") {
		t.Fatalf("expected unknown tool message, got: %q", got.Content)
	}
}

func TestLookup_NilRegistry(t *testing.T) {
	tool := New(nil)
	got, _ := tool.Run(context.Background(), map[string]any{"name": "x"})
	if !got.IsError {
		t.Fatalf("expected error result")
	}
}
