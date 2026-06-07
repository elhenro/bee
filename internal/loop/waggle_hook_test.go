package loop

import (
	"context"
	"os"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
	"github.com/elhenro/bee/internal/waggle"
)

// fakeRO is a stub read-only tool for exercising the forage hook.
type fakeRO struct{ name string }

func (f fakeRO) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        f.name,
		Description: "stub",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"pattern": map[string]any{"type": "string"},
			},
		},
	}
}

func (f fakeRO) Run(context.Context, map[string]any) (tools.Result, error) {
	return tools.Result{Content: "out"}, nil
}

func TestRunOne_CrystallizesRepeatedRoute(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	store, _ := waggle.ProjectStore("/p")
	reg := tools.NewRegistry()
	_ = reg.Register(fakeRO{"ls"})
	_ = reg.Register(fakeRO{"read"})
	e := &Engine{
		Tools:  reg,
		Waggle: waggle.NewManager(store, waggle.ManagerConfig{Scope: waggle.ScopeProject, MinePeriod: 4}),
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		e.runOne(ctx, types.ToolUse{ID: "1", Name: "ls", Input: map[string]any{"path": "/p"}})
		e.runOne(ctx, types.ToolUse{ID: "2", Name: "read", Input: map[string]any{"path": "/p/a.go"}})
	}
	ents, err := os.ReadDir(store.Dir())
	if err != nil || len(ents) != 1 {
		t.Fatalf("expected 1 crystallized waggle, got %d (err=%v)", len(ents), err)
	}
}
