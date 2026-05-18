package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

func makeProfileCfg(name string, format string) config.Config {
	return config.Config{
		Profile: name,
		Profiles: map[string]config.Profile{
			name: {ToolFormat: format},
		},
	}
}

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
	out := stripToolSpecDescriptionsForProfile(specs, makeProfileCfg("tiny", ""))
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
	out := stripToolSpecDescriptionsForProfile(specs, makeProfileCfg("normal", ""))
	if out[0].Schema["description"] != "keep" {
		t.Fatal("normal profile must not strip")
	}
}

// tiny + tool_format=xml short-circuits the strip entirely. The textmode
// wrapper nils Tools before they reach the wire, so any schema work is
// wasted.
func TestStripToolSpecDescriptions_TinyXMLNoOp(t *testing.T) {
	specs := []llm.ToolSpec{
		{Name: "a", Schema: map[string]any{"type": "object", "description": "keep"}},
	}
	out := stripToolSpecDescriptionsForProfile(specs, makeProfileCfg("tiny", "xml"))
	if out[0].Schema["description"] != "keep" {
		t.Fatal("tiny+xml should skip stripping: textmode wrapper nils Tools downstream")
	}
}
