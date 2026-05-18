package usertool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	// args reach the command as positional params; reference via "$@"
	tt, _ := New("g", `echo "$@"`, "")
	res, _ := tt.Run(context.Background(), map[string]any{"args": "world"})
	if !strings.Contains(res.Content, "world") {
		t.Errorf("args missing: %q", res.Content)
	}
}

func TestUsertool_InjectionBlocked(t *testing.T) {
	marker := filepath.Join(t.TempDir(), fmt.Sprintf("usertool_pwned_%d", os.Getpid()))
	tt, err := New("g", `echo "$@"`, "")
	if err != nil {
		t.Fatal(err)
	}
	// classic injection payload, must be treated as literal data
	payload := "; touch " + marker
	res, err := tt.Run(context.Background(), map[string]any{"args": payload})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("injection succeeded: marker %s exists", marker)
	}
	// payload should appear in output as literal text echoed back
	if !strings.Contains(res.Content, "touch") {
		t.Errorf("payload not echoed literally: %q", res.Content)
	}
}

func TestUsertool_NonStringArgsRejected(t *testing.T) {
	tt, _ := New("g", "echo ok", "")
	res, err := tt.Run(context.Background(), map[string]any{"args": []any{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("expected IsError for non-string args, got: %q", res.Content)
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
