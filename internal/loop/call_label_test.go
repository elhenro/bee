package loop

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

func labelSpecs() []llm.ToolSpec {
	schema := func() map[string]any {
		return map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}
	}
	return []llm.ToolSpec{
		{Name: "bash", Schema: schema()},
		{Name: "read", Schema: schema()},
	}
}

func TestAddCallLabels_OffersLabelOnLabeledToolsOnly(t *testing.T) {
	specs := labelSpecs()
	out := addDisplayArgs(specs)
	bash := out[0].Schema["properties"].(map[string]any)
	if _, ok := bash[types.CallLabelKey]; !ok {
		t.Fatal("bash should offer a label")
	}
	if _, ok := out[1].Schema["properties"].(map[string]any)[types.CallLabelKey]; ok {
		t.Fatal("read has a self-describing card and should not")
	}
	if _, ok := specs[0].Schema["properties"].(map[string]any)[types.CallLabelKey]; ok {
		t.Fatal("registry spec was edited in place")
	}
}

func TestAddCallLabels_KeepsAnExistingDescriptionArg(t *testing.T) {
	specs := []llm.ToolSpec{{Name: "bash", Schema: map[string]any{
		"properties": map[string]any{"description": map[string]any{"type": "string", "description": "own"}},
	}}}
	out := addDisplayArgs(specs)
	got := out[0].Schema["properties"].(map[string]any)["description"].(map[string]any)["description"]
	if got != "own" {
		t.Fatalf("existing description arg replaced: %v", got)
	}
}

// the same command worded differently each time is still the same call
func TestSignatureFor_IgnoresCallLabel(t *testing.T) {
	a := types.ToolUse{Name: "bash", Input: map[string]any{"command": "go test ./...", "description": "running tests"}}
	b := types.ToolUse{Name: "bash", Input: map[string]any{"command": "go test ./...", "description": "checking the suite"}}
	c := types.ToolUse{Name: "bash", Input: map[string]any{"command": "go test ./..."}}
	if signatureFor(a) != signatureFor(b) || signatureFor(a) != signatureFor(c) {
		t.Fatal("label changed the call signature, repeat detection would miss the loop")
	}
}

func TestRunOne_ToolNeverSeesCallLabel(t *testing.T) {
	reg := tools.NewRegistry()
	var seen map[string]any
	_ = reg.Register(&stubTool{name: "bash", desc: "x", fn: func(_ context.Context, in map[string]any) (tools.Result, error) {
		seen = in
		return tools.Result{Content: "ok"}, nil
	}})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u1", Name: "bash", Input: map[string]any{"command": "ls", "description": "listing files"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := seen[types.CallLabelKey]; leaked {
		t.Fatalf("tool received the label: %v", seen)
	}
	if seen["command"] != "ls" {
		t.Fatalf("tool lost its command: %v", seen)
	}
	kept := false
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Type == types.BlockToolUse && b.Use != nil && b.Use.Input[types.CallLabelKey] == "listing files" {
				kept = true
			}
		}
	}
	if !kept {
		t.Fatal("transcript lost the label the card needs")
	}
}

func TestAddDisplayArgs_OffersWhyOnMutators(t *testing.T) {
	specs := []llm.ToolSpec{{Name: "edit", Schema: labelSpecs()[0].Schema}, {Name: "read", Schema: labelSpecs()[1].Schema}}
	out := addDisplayArgs(specs)
	if _, ok := out[0].Schema["properties"].(map[string]any)[types.WhyKey]; !ok {
		t.Fatal("edit should offer a why")
	}
	if _, ok := out[1].Schema["properties"].(map[string]any)[types.WhyKey]; ok {
		t.Fatal("read has nothing to justify")
	}
	if _, ok := specs[0].Schema["properties"].(map[string]any)[types.WhyKey]; ok {
		t.Fatal("registry spec was edited in place")
	}
}

// a retried failing edit with reworded reasons is still the same call
func TestSignatureFor_IgnoresWhy(t *testing.T) {
	a := types.ToolUse{Name: "edit", Input: map[string]any{"path": "a.go", "old": "x", "new": "y", "why": "fix the bug"}}
	b := types.ToolUse{Name: "edit", Input: map[string]any{"path": "a.go", "old": "x", "new": "y", "why": "try again"}}
	if signatureFor(a) != signatureFor(b) {
		t.Fatal("why changed the call signature, repeat detection would miss the loop")
	}
}
