package waggle

import (
	"os"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/skills"
)

func sh(cmd string, verified bool) Call {
	return Call{Tool: "bash", Args: map[string]string{"command": cmd}, Mutates: true, Verified: verified}
}

func verified(c Call) Call { c.Verified = true; return c }

func TestRecorder_MarkOutcome(t *testing.T) {
	r := NewRecorder(0)
	r.Record(sh("a", false))
	r.MarkOutcome(true)
	r.Record(sh("b", false))
	r.Record(rd("x"))
	r.MarkOutcome(false)
	r.Record(sh("c", false))
	got := r.Calls()
	if len(got) != 2 || !got[0].Verified || got[0].Args["command"] != "a" || got[1].Verified || got[1].Args["command"] != "c" {
		t.Fatalf("calls: %+v", got)
	}
}

func TestMine_MutatingNeedsFlagVerificationAndShellOnly(t *testing.T) {
	route := func(v bool) []Call { return []Call{sh("go test ./...", v), sh("git add -A", v)} }
	calls := append(append(route(true), rd("z")), route(true)...)
	if got := Mine(calls, MineConfig{K: 2}); len(got) != 0 {
		t.Fatalf("mutating windows must stay out without the flag: %+v", got)
	}
	got := Mine(calls, MineConfig{K: 2, Mutating: true})
	if len(got) != 1 || len(got[0].Steps) != 2 || got[0].Count != 2 {
		t.Fatalf("want one 2-step route seen twice, got %+v", got)
	}
	unv := append(append(route(false), rd("z")), route(false)...)
	if got := Mine(unv, MineConfig{K: 2, Mutating: true}); len(got) != 0 {
		t.Fatalf("unverified mutating windows must not mine: %+v", got)
	}
	edit := verified(Call{Tool: "edit", Args: map[string]string{"path": "f", "old": "a", "new": "b"}, Mutates: true})
	ed := []Call{sh("go test", true), edit, rd("z"), sh("go test", true), edit}
	if got := Mine(ed, MineConfig{K: 2, Mutating: true}); len(got) != 0 {
		t.Fatalf("file edits are content, not procedure: %+v", got)
	}
}

func TestMine_DistinctShellCommandsAreDistinctShapes(t *testing.T) {
	calls := []Call{sh("a", true), sh("b", true), rd("z"), sh("c", true), sh("d", true)}
	if got := Mine(calls, MineConfig{K: 2, Mutating: true}); len(got) != 0 {
		t.Fatalf("different commands must not group as one route: %+v", got)
	}
}

func TestRenderRecipe_ParsesToRuntimeRecipe(t *testing.T) {
	cand := Candidate{
		Steps:  []Call{verified(g("foo", "")), sh("go test ./...", true)},
		Params: []Param{{Step: 0, Key: "pattern"}},
		Count:  2,
	}
	md, ok := RenderRecipe("w2", cand, ScopeProject)
	if !ok {
		t.Fatal("render failed")
	}
	s, err := skills.Parse("w2.md", []byte(md))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, md)
	}
	if s.Kind != skills.KindRecipe || !s.Runtime || len(s.Steps) != 2 {
		t.Fatalf("want runtime recipe with 2 steps: kind=%s runtime=%v steps=%d", s.Kind, s.Runtime, len(s.Steps))
	}
	if s.Steps[0].Kind != skills.StepShell || s.Steps[0].Run != "grep -rn -e {{argv.0}} -- ." {
		t.Errorf("step0: %+v", s.Steps[0])
	}
	if s.Steps[1].Run != "go test ./..." || len(s.Exec) != 0 {
		t.Errorf("step1: %+v exec=%v", s.Steps[1], s.Exec)
	}
	if strings.Contains(md, "route:") {
		t.Error("recipe waggle must not carry a replayable route")
	}
}

func TestRenderRecipe_RefusesVaryingShellCommand(t *testing.T) {
	cand := Candidate{Steps: []Call{sh("x", true), verified(rd("a"))}, Params: []Param{{Step: 0, Key: "command"}}}
	if _, ok := RenderRecipe("w", cand, ScopeProject); ok {
		t.Fatal("a parameterized raw command has no safe shape")
	}
}

func TestManager_VerifiedMutatingRouteBecomesRecipeWaggle(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	store, _ := ProjectStore("/p")
	m := NewManager(store, ManagerConfig{Scope: ScopeProject, MinePeriod: 100, Mine: MineConfig{Mutating: true}})
	for range 2 {
		m.Observe(sh("go test ./...", false))
		m.Observe(sh("git add -A", false))
		m.Observe(rd("pad"))
	}
	m.sweep()
	if n := countMD(t, store.Dir()); n != 0 {
		t.Fatalf("unverified route promoted: %d files", n)
	}
	m.MarkOutcome(true)
	m.sweep()
	metas, err := List(store)
	if err != nil || len(metas) != 1 || metas[0].Kind != "recipe" {
		t.Fatalf("want one recipe waggle, got %+v err=%v", metas, err)
	}
	if !strings.Contains(metas[0].Script, "go test ./...; git add -A") {
		t.Errorf("script: %q", metas[0].Script)
	}
	if routes, _ := LoadRoutes(store); len(routes) != 0 {
		t.Fatalf("recipe waggle must never load as a replay route: %+v", routes)
	}
	if removed, _ := GC(store); removed != 0 {
		t.Fatal("gc must keep a recipe waggle")
	}
	if _, err := os.Stat(metas[0].Path); err != nil {
		t.Fatal(err)
	}
}

func TestManager_DroppedOutcomeForgetsRoute(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	store, _ := ProjectStore("/p")
	m := NewManager(store, ManagerConfig{Scope: ScopeProject, MinePeriod: 100, Mine: MineConfig{Mutating: true}})
	for range 2 {
		m.Observe(sh("rm -rf build", false))
		m.Observe(sh("make", false))
		m.Observe(rd("pad"))
	}
	m.MarkOutcome(false)
	m.sweep()
	if n := countMD(t, store.Dir()); n != 0 {
		t.Fatalf("rolled-back route promoted: %d files", n)
	}
}

func TestRender_RefusesSecretBearingRoute(t *testing.T) {
	secret := "curl -H 'Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz0123456789' https://x.test"
	rec := Candidate{Steps: []Call{sh(secret, true), verified(rd("a"))}, Count: 2}
	if _, ok := RenderRecipe("w", rec, ScopeProject); ok {
		t.Fatal("recipe route carrying a secret must not be crystallized")
	}
	exec := Candidate{Steps: []Call{g("API_KEY=hunter2hunter2", ""), rd("a.go")}, Count: 2}
	if _, ok := Render("w", exec, ScopeProject); ok {
		t.Fatal("exec route carrying a secret must not be crystallized")
	}
}
