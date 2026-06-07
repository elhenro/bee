package waggle

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func okExec(out string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) { return out, nil }
}

func TestReplay_RecordsDivergenceOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	rt := litRoute()
	rt.Scope = ScopeProject
	r := NewReplayer([]Route{rt}, 2)
	r.SetLedger(ScopeProject, NewLedger(path))
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": "internal"}})
	r.Observe(rd("a.go"))
	failExec := func(context.Context, string) (string, error) { return "", errors.New("boom") }
	if _, ok := r.Follow(context.Background(), failExec); ok {
		t.Fatal("failing exec must not fire")
	}
	got := mustReadLedger(t, path)["wag_x"]
	if got.Fails != 1 || got.Uses != 0 {
		t.Errorf("divergence not recorded: %+v", got)
	}
}

func mustReadLedger(t *testing.T, path string) map[string]Stat {
	t.Helper()
	stats, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

// a literal route long enough to leave a tail after a 2-step prefix.
func litRoute() Route {
	return Route{Name: "wag_x", Steps: []Call{
		{Tool: "ls", Args: map[string]string{"path": "internal"}},
		rd("a.go"),
		rd("b.go"),
	}}
}

func TestReplay_FollowsLiteralTail(t *testing.T) {
	r := NewReplayer([]Route{litRoute()}, 2)
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": "internal"}})
	r.Observe(rd("a.go"))

	var ran []string
	exec := func(_ context.Context, script string) (string, error) {
		ran = append(ran, script)
		return "B-CONTENT", nil
	}
	block, ok := r.Follow(context.Background(), exec)
	if !ok {
		t.Fatal("expected replay to fire on matched prefix")
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "cat 'b.go'") {
		t.Fatalf("tail script wrong: %v", ran)
	}
	if !strings.Contains(block, "B-CONTENT") || !strings.Contains(block, "wag_x") {
		t.Errorf("block missing content/name: %q", block)
	}
	if r.Yield() <= 0 {
		t.Errorf("yield not accumulated: %d", r.Yield())
	}
}

func TestReplay_DedupeNoDoubleFire(t *testing.T) {
	r := NewReplayer([]Route{litRoute()}, 2)
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": "internal"}})
	r.Observe(rd("a.go"))
	if _, ok := r.Follow(context.Background(), okExec("OUT")); !ok {
		t.Fatal("first follow should fire")
	}
	if _, ok := r.Follow(context.Background(), okExec("OUT")); ok {
		t.Fatal("identical plan must not fire twice")
	}
}

func TestReplay_NoFireBelowMinPrefix(t *testing.T) {
	r := NewReplayer([]Route{litRoute()}, 2)
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": "internal"}}) // 1 call only
	if _, ok := r.Follow(context.Background(), okExec("OUT")); ok {
		t.Fatal("single observed call must not fire a 2-step prefix")
	}
}

func TestReplay_SkipsParamTail(t *testing.T) {
	r := NewReplayer([]Route{{
		Name: "wag_p",
		Steps: []Call{
			{Tool: "ls", Args: map[string]string{"path": "x"}},
			rd("a.go"),
			{Tool: "read", Args: map[string]string{"path": "$1"}},
		},
		Params: []Param{{Step: 2, Key: "path"}},
	}}, 2)
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": "x"}})
	r.Observe(rd("a.go"))
	if _, ok := r.Follow(context.Background(), okExec("OUT")); ok {
		t.Fatal("tail with an unbound param must not replay")
	}
}

func TestReplay_NoMatchUnrelated(t *testing.T) {
	r := NewReplayer([]Route{litRoute()}, 2)
	r.Observe(rd("zzz.go"))
	r.Observe(rd("qqq.go"))
	if _, ok := r.Follow(context.Background(), okExec("OUT")); ok {
		t.Fatal("unrelated calls must not fire")
	}
}

func TestReplay_LiteralValueMustMatch(t *testing.T) {
	r := NewReplayer([]Route{litRoute()}, 2)
	// same shape (ls,read) but different ls path -> not this route
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": "OTHER"}})
	r.Observe(rd("a.go"))
	if _, ok := r.Follow(context.Background(), okExec("OUT")); ok {
		t.Fatal("literal mismatch must not fire")
	}
}

func TestLoadRoutes_RoundTrip(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	cand := Candidate{Steps: []Call{
		{Tool: "ls", Args: map[string]string{"path": "internal"}},
		rd("internal/a.go"),
		g("foo", "src"),
	}, Count: 2, Params: []Param{{Step: 2, Key: "pattern"}}}
	md, ok := Render("wag_rt", cand, ScopeProject)
	if !ok {
		t.Fatal("render failed")
	}
	if err := s.Write("wag_rt", md); err != nil {
		t.Fatal(err)
	}
	routes, err := LoadRoutes(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("want 1 route, got %d", len(routes))
	}
	got := routes[0]
	if got.Name != "wag_rt" || len(got.Steps) != 3 {
		t.Fatalf("bad route: %+v", got)
	}
	if got.Steps[0].Tool != "ls" || got.Steps[0].Args["path"] != "internal" {
		t.Errorf("step0 literal lost: %+v", got.Steps[0])
	}
	if got.Steps[1].Tool != "read" || got.Steps[1].Args["path"] != "internal/a.go" {
		t.Errorf("step1 literal lost: %+v", got.Steps[1])
	}
	if len(got.Params) != 1 || got.Params[0].Step != 2 || got.Params[0].Key != "pattern" {
		t.Errorf("param not recovered: %+v", got.Params)
	}
}

// full chain: Render -> store -> LoadRoutes -> Replayer -> real bash. Proves the
// literal tail translates to a script that actually reads the file on disk.
func TestReplay_EndToEndRealShell(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("BRAVO-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	cand := Candidate{Steps: []Call{
		{Tool: "ls", Args: map[string]string{"path": work}},
		{Tool: "read", Args: map[string]string{"path": filepath.Join(work, "a.txt")}},
		{Tool: "read", Args: map[string]string{"path": filepath.Join(work, "b.txt")}},
	}, Count: 2}
	md, ok := Render("wag_e2e", cand, ScopeProject)
	if !ok {
		t.Fatal("render failed")
	}
	if err := s.Write("wag_e2e", md); err != nil {
		t.Fatal(err)
	}
	routes, err := LoadRoutes(s)
	if err != nil || len(routes) != 1 {
		t.Fatalf("load routes: %v (%d)", err, len(routes))
	}
	r := NewReplayer(routes, 2)
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": work}})
	r.Observe(Call{Tool: "read", Args: map[string]string{"path": filepath.Join(work, "a.txt")}})

	run := func(ctx context.Context, script string) (string, error) {
		out, err := exec.CommandContext(ctx, "bash", "-c", script).CombinedOutput()
		return string(out), err
	}
	block, ok := r.Follow(context.Background(), run)
	if !ok {
		t.Fatal("expected end-to-end replay to fire")
	}
	if !strings.Contains(block, "BRAVO-CONTENT") {
		t.Errorf("real shell did not read tail file: %q", block)
	}
}

func TestLoadRoutes_MissingDir(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/never")
	routes, err := LoadRoutes(s)
	if err != nil || routes != nil {
		t.Fatalf("missing dir should be nil/nil: %v %v", routes, err)
	}
}
