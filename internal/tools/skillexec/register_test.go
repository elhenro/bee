package skillexec

import (
	"testing"

	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
)

func TestRegisterExecSkills_RegistersExecOnly(t *testing.T) {
	r := tools.NewRegistry()
	list := []skills.Skill{
		{Name: "promptish", Kind: skills.KindPrompt, Body: "hi"},
		{Name: "foo", Kind: skills.KindExec, Exec: []string{"bash", "-c", "true"}},
		{Name: "bar", Kind: skills.KindExec, Exec: []string{"bash", "-c", "true"}},
	}
	n := RegisterExecSkills(r, list)
	if n != 2 {
		t.Fatalf("expected 2 registered, got %d", n)
	}
	if _, ok := r.Get("foo"); !ok {
		t.Error("foo not registered")
	}
	if _, ok := r.Get("bar"); !ok {
		t.Error("bar not registered")
	}
	if _, ok := r.Get("promptish"); ok {
		t.Error("prompt skill must not be registered as a tool")
	}
}

func TestRegisterExecSkills_SkipsCollision(t *testing.T) {
	r := tools.NewRegistry()
	list := []skills.Skill{{Name: "dup", Kind: skills.KindExec, Exec: []string{"bash", "-c", "true"}}}
	if n := RegisterExecSkills(r, list); n != 1 {
		t.Fatalf("first pass: expected 1, got %d", n)
	}
	// second pass over the same name must skip, not clobber or panic
	if n := RegisterExecSkills(r, list); n != 0 {
		t.Fatalf("collision must be skipped, got %d registered", n)
	}
}
