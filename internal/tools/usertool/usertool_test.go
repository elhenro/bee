package usertool

import (
	"context"
	"strings"
	"testing"
)

func TestNew_RejectsEmpty(t *testing.T) {
	if _, err := New("", "echo", "d"); err == nil {
		t.Error("empty name must error")
	}
	if _, err := New("n", "", "d"); err == nil {
		t.Error("empty command must error")
	}
}

func TestRun_ExecutesCommand(t *testing.T) {
	tt, err := New("greet", "echo hello", "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := tt.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("missing output: %q", res.Content)
	}
}

func TestRun_AppendsArgs(t *testing.T) {
	tt, _ := New("g", "echo", "")
	res, _ := tt.Run(context.Background(), map[string]any{"args": "world"})
	if !strings.Contains(res.Content, "world") {
		t.Errorf("args missing: %q", res.Content)
	}
}

func TestSpec_PreservesName(t *testing.T) {
	tt, _ := New("lint", "true", "run lint")
	if tt.Spec().Name != "lint" {
		t.Errorf("name not preserved")
	}
	if !strings.Contains(tt.Spec().Description, "run lint") {
		t.Errorf("description missing")
	}
}
