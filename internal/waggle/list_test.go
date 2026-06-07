package waggle

import (
	"os"
	"path/filepath"
	"testing"
)

func seedWaggle(t *testing.T, s *Store, name string, steps []Call) {
	t.Helper()
	md, ok := Render(name, Candidate{Steps: steps, Count: 2}, ScopeProject)
	if !ok {
		t.Fatal("render failed")
	}
	if err := s.Write(name, md); err != nil {
		t.Fatal(err)
	}
}

func TestList_ReturnsCrystallized(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	seedWaggle(t, s, "w_a", []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")})
	seedWaggle(t, s, "w_b", []Call{{Tool: "ls", Args: map[string]string{"path": "y"}}, rd("b")})
	metas, err := List(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Fatalf("expected 2, got %d", len(metas))
	}
	if metas[0].Name == "" || metas[0].Script == "" {
		t.Errorf("incomplete meta: %+v", metas[0])
	}
}

func TestList_MissingDir(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/never-written")
	metas, err := List(s)
	if err != nil || metas != nil {
		t.Fatalf("missing dir should be empty: metas=%v err=%v", metas, err)
	}
}

func TestGC_RemovesDuplicateAndBroken(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	route := []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")}
	seedWaggle(t, s, "dup1", route)
	seedWaggle(t, s, "dup2", route) // identical script -> duplicate
	if err := os.WriteFile(filepath.Join(s.Dir(), "broken.md"), []byte("not valid frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := GC(s)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 removed (1 dup + 1 broken), got %d", removed)
	}
	metas, _ := List(s)
	if len(metas) != 1 {
		t.Errorf("expected 1 surviving waggle, got %d", len(metas))
	}
}
