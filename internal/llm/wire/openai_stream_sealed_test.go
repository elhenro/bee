package wire

import "testing"

func delta(idx int, id, name, args string) StreamToolCall {
	return StreamToolCall{Index: idx, ID: id, Function: StreamFunctionDelta{Name: name, Arguments: args}}
}

func TestTakeSealed_ReleasesCallOnceSiblingStarts(t *testing.T) {
	a := NewToolCallAccumulator()
	a.Apply([]StreamToolCall{delta(0, "c0", "read", `{"path":"a`)})
	if got := a.TakeSealed(); len(got) != 0 {
		t.Fatalf("sealed %d while the only call is streaming", len(got))
	}
	a.Apply([]StreamToolCall{delta(0, "", "", `.go"}`)})
	if got := a.TakeSealed(); len(got) != 0 {
		t.Fatalf("sealed %d with no later call started: it may still be growing", len(got))
	}
	a.Apply([]StreamToolCall{delta(1, "c1", "search", `{"pattern":"x`)})
	got := a.TakeSealed()
	if len(got) != 1 || got[0].ID != "c0" || got[0].Input["path"] != "a.go" {
		t.Fatalf("sealed = %+v, want c0 with path a.go", got)
	}
	if again := a.TakeSealed(); len(again) != 0 {
		t.Fatalf("sealed call handed out twice: %+v", again)
	}
	a.Apply([]StreamToolCall{delta(1, "", "", `"}`)})
	rest, _ := a.Finalize()
	if len(rest) != 1 || rest[0].ID != "c1" || rest[0].Input["pattern"] != "x" {
		t.Fatalf("Finalize = %+v, want only c1", rest)
	}
}

// an incomplete earlier call blocks later ones: emission order is preserved.
func TestTakeSealed_KeepsOrder(t *testing.T) {
	a := NewToolCallAccumulator()
	a.Apply([]StreamToolCall{
		delta(0, "c0", "read", `{"path":`),
		delta(1, "c1", "ls", `{"path":"."}`),
		delta(2, "c2", "ls", `{`),
	})
	if got := a.TakeSealed(); len(got) != 0 {
		t.Fatalf("sealed %+v past an incomplete call", got)
	}
	a.Apply([]StreamToolCall{delta(0, "", "", `"a"}`)})
	got := a.TakeSealed()
	if len(got) != 2 || got[0].ID != "c0" || got[1].ID != "c1" {
		t.Fatalf("sealed = %+v, want c0 then c1", got)
	}
}

func TestTakeSealed_EmptyArgsWaitForFinalize(t *testing.T) {
	a := NewToolCallAccumulator()
	a.Apply([]StreamToolCall{delta(0, "c0", "ls", ""), delta(1, "c1", "ls", `{}`)})
	if got := a.TakeSealed(); len(got) != 0 {
		t.Fatalf("sealed %+v with empty args: more may follow", got)
	}
	all, _ := a.Finalize()
	if len(all) != 2 {
		t.Fatalf("Finalize = %d calls, want 2", len(all))
	}
}
