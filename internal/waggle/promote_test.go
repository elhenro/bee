package waggle

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/skills"
)

func TestRender_ExactRepeatParsesToExecSkill(t *testing.T) {
	cand := Candidate{Steps: []Call{g("foo", ""), rd("a.go")}, Count: 2}
	md, ok := Render("w1", cand, ScopeProject)
	if !ok {
		t.Fatal("render failed")
	}
	s, err := skills.Parse("w1.md", []byte(md))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, md)
	}
	if s.Kind != skills.KindExec {
		t.Fatalf("kind = %v, want exec", s.Kind)
	}
	if len(s.Exec) != 3 || s.Exec[0] != "bash" || s.Exec[1] != "-c" {
		t.Fatalf("exec vector: %v", s.Exec)
	}
	script := s.Exec[2]
	if !strings.Contains(script, "grep -rn 'foo'") || !strings.Contains(script, "cat 'a.go'") {
		t.Errorf("script missing steps: %q", script)
	}
}

func TestRender_WithParam(t *testing.T) {
	cand := Candidate{Steps: []Call{g("foo", "src")}, Count: 2, Params: []Param{{Step: 0, Key: "pattern"}}}
	md, ok := Render("w2", cand, ScopeProject)
	if !ok {
		t.Fatal("render failed")
	}
	s, err := skills.Parse("w2.md", []byte(md))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(s.Exec[2], `"$1"`) {
		t.Errorf("param token missing in script: %q", s.Exec[2])
	}
	if !strings.Contains(md, "params:") || !strings.Contains(md, "pattern") {
		t.Errorf("params frontmatter missing:\n%s", md)
	}
}

func TestRender_UntranslatableStep(t *testing.T) {
	cand := Candidate{Steps: []Call{{Tool: "browser", Args: map[string]string{}}}, Count: 2}
	if _, ok := Render("w3", cand, ScopeProject); ok {
		t.Fatal("untranslatable route must not render")
	}
}
