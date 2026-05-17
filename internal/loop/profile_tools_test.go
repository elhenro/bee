package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/llm"
)

func TestStripSchemaDescriptions_TinyOnly(t *testing.T) {
	in := map[string]any{
		"type":        "object",
		"description": "outer",
		"properties": map[string]any{
			"x": map[string]any{"type": "string", "description": "inner"},
		},
	}
	out := StripSchemaDescriptionsForProfile(in, "tiny")
	if _, ok := out["description"]; ok {
		t.Fatal("outer description leaked")
	}
	p := out["properties"].(map[string]any)["x"].(map[string]any)
	if _, ok := p["description"]; ok {
		t.Fatal("inner description leaked")
	}

	out2 := StripSchemaDescriptionsForProfile(in, "normal")
	if out2["description"] != "outer" {
		t.Fatal("normal profile should keep description")
	}
}

func TestStripSchemaDescriptions_HandlesNestedSlices(t *testing.T) {
	in := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string", "description": "a"},
			map[string]any{"type": "number", "description": "b"},
		},
	}
	out := StripSchemaDescriptionsForProfile(in, "tiny")
	arr, ok := out["oneOf"].([]any)
	if !ok {
		t.Fatal("oneOf not preserved")
	}
	for i, el := range arr {
		m := el.(map[string]any)
		if _, ok := m["description"]; ok {
			t.Errorf("description leaked at oneOf[%d]", i)
		}
	}
}

func TestStripToolSpecDescriptions_TinyAllSpecs(t *testing.T) {
	specs := []llm.ToolSpec{
		{Name: "a", Schema: map[string]any{"type": "object", "description": "ta"}},
		{Name: "b", Schema: map[string]any{"type": "object", "description": "tb"}},
	}
	out := stripToolSpecDescriptionsForProfile(specs, "tiny")
	for i, s := range out {
		if _, ok := s.Schema["description"]; ok {
			t.Errorf("spec %d description leaked", i)
		}
	}
	// original specs untouched (deep clone, not in-place mutation)
	for i, s := range specs {
		if _, ok := s.Schema["description"]; !ok {
			t.Errorf("original spec %d mutated in place", i)
		}
	}
}

func TestStripToolSpecDescriptions_NormalPassthrough(t *testing.T) {
	specs := []llm.ToolSpec{
		{Name: "a", Schema: map[string]any{"type": "object", "description": "keep"}},
	}
	out := stripToolSpecDescriptionsForProfile(specs, "normal")
	if out[0].Schema["description"] != "keep" {
		t.Fatal("normal profile must not strip")
	}
}
