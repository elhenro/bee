package waggle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneStale_RemovesZeroUseOld(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	seedWaggle(t, s, "old_unused", []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")})
	seedWaggle(t, s, "old_used", []Call{{Tool: "ls", Args: map[string]string{"path": "y"}}, rd("b")})
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, n := range []string{"old_unused", "old_used"} {
		if err := os.Chtimes(filepath.Join(s.Dir(), n+".md"), old, old); err != nil {
			t.Fatal(err)
		}
	}
	stats := map[string]Stat{"old_used": {Uses: 3, Yield: 99}}
	removed, err := PruneStale(s, stats, 14*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 pruned, got %d", removed)
	}
	if s.Exists("old_unused") {
		t.Error("zero-use stale waggle should be gone")
	}
	if !s.Exists("old_used") {
		t.Error("used waggle must be kept regardless of age")
	}
}

func TestPruneStale_KeepsRecent(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	seedWaggle(t, s, "fresh", []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")})
	removed, err := PruneStale(s, map[string]Stat{}, 14*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 || !s.Exists("fresh") {
		t.Fatalf("recent zero-use waggle must be kept: removed=%d", removed)
	}
}

func TestPromote_CrossProjectRouteToUser(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	shared := []Call{{Tool: "ls", Args: map[string]string{"path": "internal"}}, rd("a.go")}
	only := []Call{{Tool: "ls", Args: map[string]string{"path": "cmd"}}, rd("main.go")}
	p1, _ := ProjectStore("/proj/one")
	p2, _ := ProjectStore("/proj/two")
	seedWaggle(t, p1, "shared", shared) // same route...
	seedWaggle(t, p2, "shared", shared) // ...in a second project
	seedWaggle(t, p1, "only", only)     // route in just one project

	user, _ := UserStore()
	n, err := Promote(user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 promoted (cross-project route), got %d", n)
	}
	routes, _ := LoadRoutes(user)
	if len(routes) != 1 {
		t.Fatalf("user store should hold exactly the promoted route, got %d", len(routes))
	}
	if n2, _ := Promote(user); n2 != 0 {
		t.Errorf("promote must be idempotent, got %d", n2)
	}
}
