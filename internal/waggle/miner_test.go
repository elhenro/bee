package waggle

import "testing"

func g(pattern, path string) Call {
	a := map[string]string{"pattern": pattern}
	if path != "" {
		a["path"] = path
	}
	return Call{Tool: "search", Args: a} // bee's grep tool is named "search"
}

func rd(path string) Call { return Call{Tool: "read", Args: map[string]string{"path": path}} }

func TestMine_FindsExactRepeatedRoute(t *testing.T) {
	calls := []Call{g("foo", ""), rd("a.go"), g("foo", ""), rd("a.go")}
	cands := Mine(calls, MineConfig{MinLen: 2, MaxLen: 6, K: 2})
	if len(cands) == 0 {
		t.Fatal("expected a candidate")
	}
	c := cands[0]
	if len(c.Steps) != 2 || c.Steps[0].Tool != "search" || c.Steps[1].Tool != "read" {
		t.Fatalf("unexpected steps: %+v", c.Steps)
	}
	if c.Count != 2 {
		t.Errorf("expected count 2, got %d", c.Count)
	}
	if len(c.Params) != 0 {
		t.Errorf("exact repeat must have no params, got %+v", c.Params)
	}
}

func TestMine_ExtractsParams(t *testing.T) {
	calls := []Call{g("foo", "src"), rd("a"), g("bar", "src"), rd("a")}
	cands := Mine(calls, MineConfig{MinLen: 2, MaxLen: 6, K: 2})
	if len(cands) == 0 {
		t.Fatal("expected a candidate")
	}
	c := cands[0]
	if len(c.Params) != 1 {
		t.Fatalf("expected 1 param (grep.pattern varies), got %+v", c.Params)
	}
	p := c.Params[0]
	if p.Step != 0 || p.Key != "pattern" {
		t.Errorf("wrong param: %+v", p)
	}
}

func TestMine_RejectsMutatorWindow(t *testing.T) {
	w := Call{Tool: "write", Args: map[string]string{"path": "x"}, Mutates: true}
	calls := []Call{g("foo", ""), w, g("foo", ""), w}
	cands := Mine(calls, MineConfig{MinLen: 2, MaxLen: 6, K: 2})
	for _, c := range cands {
		for _, s := range c.Steps {
			if s.Mutates || s.Tool == "write" {
				t.Fatalf("candidate must not contain a mutator: %+v", c.Steps)
			}
		}
	}
}

func TestMine_RequiresKOccurrences(t *testing.T) {
	calls := []Call{g("foo", ""), rd("a.go")}
	cands := Mine(calls, MineConfig{MinLen: 2, MaxLen: 6, K: 2})
	if len(cands) != 0 {
		t.Fatalf("single occurrence must not be a candidate, got %+v", cands)
	}
}

func TestMine_NonOverlappingCount(t *testing.T) {
	calls := []Call{rd("a"), rd("a"), rd("a")}
	cands := Mine(calls, MineConfig{MinLen: 2, MaxLen: 6, K: 2})
	if len(cands) != 0 {
		t.Fatalf("overlapping repeats must not inflate count, got %+v", cands)
	}
}
