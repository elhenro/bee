package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
	"github.com/elhenro/bee/internal/waggle"
)

// fakeBash records the command it was asked to run and returns canned output,
// standing in for the real shell tool so replay's tail exec is observable.
type fakeBash struct{ got *[]string }

func (f fakeBash) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "bash",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"command": map[string]any{"type": "string"}},
		},
	}
}

func (f fakeBash) Run(_ context.Context, in map[string]any) (tools.Result, error) {
	cmd, _ := in["command"].(string)
	*f.got = append(*f.got, cmd)
	return tools.Result{Content: "B-FILE-CONTENT"}, nil
}

func TestDispatchTools_ReplayPrefetchesTail(t *testing.T) {
	var bashGot []string
	reg := tools.NewRegistry()
	_ = reg.Register(fakeRO{"ls"})
	_ = reg.Register(fakeRO{"read"})
	_ = reg.Register(fakeBash{&bashGot})

	route := waggle.Route{Name: "wag_x", Steps: []waggle.Call{
		{Tool: "ls", Args: map[string]string{"path": "internal"}},
		{Tool: "read", Args: map[string]string{"path": "a.go"}},
		{Tool: "read", Args: map[string]string{"path": "b.go"}},
	}}
	e := &Engine{Tools: reg, Replay: waggle.NewReplayer([]waggle.Route{route}, 2)}

	uses := []types.ToolUse{
		{ID: "1", Name: "ls", Input: map[string]any{"path": "internal"}},
		{ID: "2", Name: "read", Input: map[string]any{"path": "a.go"}},
	}
	results, err := e.dispatchTools(context.Background(), uses)
	if err != nil {
		t.Fatal(err)
	}
	if len(bashGot) != 1 || !strings.Contains(bashGot[0], "cat 'b.go'") {
		t.Fatalf("tail not executed via shell: %v", bashGot)
	}
	if !strings.Contains(results[1].Content, "B-FILE-CONTENT") || !strings.Contains(results[1].Content, "wag_x") {
		t.Fatalf("prefetch not folded into triggering result: %q", results[1].Content)
	}
	if strings.Contains(results[0].Content, "B-FILE-CONTENT") {
		t.Errorf("prefetch attached to the wrong result")
	}
}

func TestDispatchTools_ReplayNoMatchNoExec(t *testing.T) {
	var bashGot []string
	reg := tools.NewRegistry()
	_ = reg.Register(fakeRO{"read"})
	_ = reg.Register(fakeBash{&bashGot})

	route := waggle.Route{Name: "wag_x", Steps: []waggle.Call{
		{Tool: "ls", Args: map[string]string{"path": "internal"}},
		{Tool: "read", Args: map[string]string{"path": "a.go"}},
		{Tool: "read", Args: map[string]string{"path": "b.go"}},
	}}
	e := &Engine{Tools: reg, Replay: waggle.NewReplayer([]waggle.Route{route}, 2)}

	uses := []types.ToolUse{
		{ID: "1", Name: "read", Input: map[string]any{"path": "unrelated.go"}},
	}
	if _, err := e.dispatchTools(context.Background(), uses); err != nil {
		t.Fatal(err)
	}
	if len(bashGot) != 0 {
		t.Fatalf("replay fired on unrelated calls: %v", bashGot)
	}
}
