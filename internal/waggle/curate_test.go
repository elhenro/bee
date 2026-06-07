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

func TestDemote_DisablesChronicallyDiverging(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	seedWaggle(t, s, "flaky", []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")})
	seedWaggle(t, s, "solid", []Call{{Tool: "ls", Args: map[string]string{"path": "y"}}, rd("b")})

	// flaky diverged repeatedly and never paid off; solid diverged but also hit.
	stats := map[string]Stat{
		"flaky": {Fails: 3, Uses: 0},
		"solid": {Fails: 5, Uses: 1},
	}
	n, err := Demote(s, stats, 3)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 demoted, got %d", n)
	}
	if !s.Exists("flaky") {
		t.Error("demoted waggle file should be kept (disabled, not deleted)")
	}
	routes, _ := LoadRoutes(s)
	if len(routes) != 1 || routes[0].Name != "solid" {
		t.Fatalf("disabled route must not load; want only solid, got %+v", routes)
	}
	if n2, _ := Demote(s, stats, 3); n2 != 0 {
		t.Errorf("demote must be idempotent, got %d", n2)
	}
}

func TestPromote_SkipsDisabledHolders(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	shared := []Call{{Tool: "ls", Args: map[string]string{"path": "internal"}}, rd("a.go")}
	p1, _ := ProjectStore("/proj/one")
	p2, _ := ProjectStore("/proj/two")
	seedWaggle(t, p1, "shared", shared)
	seedWaggle(t, p2, "shared", shared)
	// route diverged in both projects: disabled everywhere, not portable.
	mustDisable(t, p1, "shared")
	mustDisable(t, p2, "shared")
	user, _ := UserStore()
	n, err := Promote(user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("disabled holders must not promote, got %d", n)
	}
}

func TestPromote_MixedDisabledStillPromotesEnabled(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	shared := []Call{{Tool: "ls", Args: map[string]string{"path": "internal"}}, rd("a.go")}
	p1, _ := ProjectStore("/proj/one")
	p2, _ := ProjectStore("/proj/two")
	p3, _ := ProjectStore("/proj/three")
	seedWaggle(t, p1, "shared", shared)
	seedWaggle(t, p2, "shared", shared)
	seedWaggle(t, p3, "shared", shared)
	mustDisable(t, p3, "shared") // diverged only in p3
	user, _ := UserStore()
	if n, _ := Promote(user); n != 1 {
		t.Fatalf("two active holders should promote once, got %d", n)
	}
	routes, _ := LoadRoutes(user)
	if len(routes) != 1 {
		t.Fatalf("promoted user route must be enabled/loadable, got %d", len(routes))
	}
}

func TestCurate_DemotesLongLivedDivergerBeforePrune(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	proj, _ := ProjectStore("/p")
	user, _ := UserStore()
	seedWaggle(t, proj, "diverger", []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")})
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(proj.Dir(), "diverger.md"), old, old); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(proj.LedgerPath())
	for i := 0; i < 3; i++ {
		_ = l.Append(LedgerEntry{Name: "diverger", Fail: true})
	}
	if _, err := Curate(proj, user, 14*24*time.Hour, 3, time.Now()); err != nil {
		t.Fatal(err)
	}
	// kept for inspection (disabled), not pruned away despite age.
	if !proj.Exists("diverger") {
		t.Fatal("long-lived diverger must be demoted (kept), not pruned")
	}
	routes, _ := LoadRoutes(proj)
	if len(routes) != 0 {
		t.Errorf("demoted route must not load for replay, got %d", len(routes))
	}
}

func TestCurate_CompactsLedgerForPrunedRoutes(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	proj, _ := ProjectStore("/p")
	user, _ := UserStore()
	seedWaggle(t, proj, "stale1fail", []Call{{Tool: "ls", Args: map[string]string{"path": "x"}}, rd("a")})
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(proj.Dir(), "stale1fail.md"), old, old); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(proj.LedgerPath())
	_ = l.Append(LedgerEntry{Name: "stale1fail", Fail: true}) // 1 fail: below demote threshold
	if _, err := Curate(proj, user, 14*24*time.Hour, 3, time.Now()); err != nil {
		t.Fatal(err)
	}
	if proj.Exists("stale1fail") {
		t.Fatal("old zero-use route below the demote threshold should be pruned")
	}
	stats, _ := ReadLedger(proj.LedgerPath())
	if _, ok := stats["stale1fail"]; ok {
		t.Error("ledger history for a pruned route must be compacted away")
	}
}

func TestSurvivingNames_PropagatesReadError(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "skills")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := survivingNames(&Store{dir: notDir}); err == nil {
		t.Fatal("non-IsNotExist read error must propagate, not be swallowed")
	}
}

func TestCurate_PreservesLedgerWhenSkillsDirUnreadable(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	proj, _ := ProjectStore("/p")
	user, _ := UserStore()
	// skills dir is a regular file: ReadDir fails for a non-IsNotExist reason.
	if err := os.MkdirAll(filepath.Dir(proj.Dir()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proj.Dir(), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(proj.LedgerPath())
	_ = l.Append(LedgerEntry{Name: "keepme", Yield: 100})
	if _, err := Curate(proj, user, 14*24*time.Hour, 3, time.Now()); err != nil {
		t.Fatal(err)
	}
	stats, _ := ReadLedger(proj.LedgerPath())
	if stats["keepme"].Yield != 100 {
		t.Errorf("ledger must survive an unreadable skills dir, got %+v", stats["keepme"])
	}
}

func mustDisable(t *testing.T, s *Store, name string) {
	t.Helper()
	if err := disableFile(filepath.Join(s.Dir(), name+".md")); err != nil {
		t.Fatal(err)
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
