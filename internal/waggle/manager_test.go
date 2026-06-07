package waggle

import (
	"os"
	"testing"
)

func countMD(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func observeRoute(m *Manager, times int) {
	for i := 0; i < times; i++ {
		m.Observe(Call{Tool: "ls", Args: map[string]string{"path": "/p"}})
		m.Observe(Call{Tool: "read", Args: map[string]string{"path": "/p/a.go"}})
	}
}

func TestManager_PromotesRepeatedRoute(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	store, _ := ProjectStore("/p")
	m := NewManager(store, ManagerConfig{Scope: ScopeProject, MinePeriod: 4})
	observeRoute(m, 2) // 4 observations -> one sweep, route seen twice
	if n := countMD(t, store.Dir()); n != 1 {
		t.Fatalf("expected 1 waggle written, got %d", n)
	}
}

func TestManager_Idempotent(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	store, _ := ProjectStore("/p")
	m := NewManager(store, ManagerConfig{Scope: ScopeProject, MinePeriod: 4})
	observeRoute(m, 4) // same route many times
	if n := countMD(t, store.Dir()); n != 1 {
		t.Fatalf("repeated identical route must not duplicate waggles, got %d", n)
	}
}

func TestManager_NoPromoteBelowK(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	store, _ := ProjectStore("/p")
	m := NewManager(store, ManagerConfig{Scope: ScopeProject, MinePeriod: 2})
	observeRoute(m, 1) // route seen once -> below K
	if n := countMD(t, store.Dir()); n != 0 {
		t.Fatalf("single occurrence must not promote, got %d", n)
	}
}
