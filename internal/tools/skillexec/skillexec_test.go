package skillexec

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/skills"
)

func TestNew_RejectsNonExecSkill(t *testing.T) {
	_, err := New(skills.Skill{Name: "p", Kind: skills.KindPrompt, Body: "hi"})
	if err == nil {
		t.Fatal("prompt-kind skill must be rejected")
	}
}

func TestNew_RejectsEmptyExec(t *testing.T) {
	_, err := New(skills.Skill{Name: "e", Kind: skills.KindExec})
	if err == nil {
		t.Fatal("exec skill with empty command must be rejected")
	}
}

func TestRun_ExecutesCommand(t *testing.T) {
	tt, err := New(skills.Skill{Name: "greet", Kind: skills.KindExec, Exec: []string{"bash", "-c", "echo hello"}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tt.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("missing output: %q", res.Content)
	}
}

func TestRun_SubstitutesPositionalArgs(t *testing.T) {
	// waggle param form: script references $1, model supplies it via args
	tt, _ := New(skills.Skill{Name: "g", Kind: skills.KindExec, Exec: []string{"bash", "-c", `echo "$1"`}})
	res, _ := tt.Run(context.Background(), map[string]any{"args": "world"})
	if !strings.Contains(res.Content, "world") {
		t.Errorf("positional arg missing: %q", res.Content)
	}
}

func TestRun_BlocksHardlineCommand(t *testing.T) {
	tt, err := New(skills.Skill{Name: "danger", Kind: skills.KindExec, Exec: []string{"bash", "-c", "rm -rf /"}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tt.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("hardline command must be refused, got: %q", res.Content)
	}
	if !strings.Contains(res.Content, "refused") {
		t.Errorf("expected refusal message, got: %q", res.Content)
	}
}

func TestSpec_UsesSkillName(t *testing.T) {
	tt, _ := New(skills.Skill{Name: "lint", Kind: skills.KindExec, Exec: []string{"bash", "-c", "true"}, Description: "run lint"})
	if tt.Spec().Name != "lint" {
		t.Errorf("spec name not preserved: %q", tt.Spec().Name)
	}
}
