package why

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

type probe struct {
	name string
	got  map[string]any
}

func (p *probe) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: p.name, Schema: map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
		"required":   []string{"path"},
	}}
}

func (p *probe) Run(_ context.Context, in map[string]any) (tools.Result, error) {
	p.got = in
	return tools.Result{Content: "ok"}, nil
}

func TestWrapAddsPropAndStripsKey(t *testing.T) {
	edit, read := &probe{name: "edit"}, &probe{name: "read"}
	all := WrapAll([]tools.Tool{edit, read})

	props := all[0].Spec().Schema["properties"].(map[string]any)
	if _, ok := props[Key]; !ok {
		t.Fatal("edit spec lacks why")
	}
	if _, ok := edit.Spec().Schema["properties"].(map[string]any)[Key]; ok {
		t.Fatal("inner schema mutated")
	}
	if all[1] != tools.Tool(read) {
		t.Fatal("non-mutator wrapped")
	}

	if _, err := all[0].Run(context.Background(), map[string]any{"path": "a", Key: "fix off-by-one"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.got[Key]; ok || edit.got["path"] != "a" {
		t.Fatalf("inner saw %v", edit.got)
	}
}

func TestOf(t *testing.T) {
	if got := Of(map[string]any{Key: "  because  "}); got != "because" {
		t.Fatalf("got %q", got)
	}
	if Of(map[string]any{}) != "" {
		t.Fatal("want empty")
	}
}
