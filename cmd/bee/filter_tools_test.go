package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestFilterTools_subsetKeepsNamed(t *testing.T) {
	reg, err := buildTools(t.TempDir(), config.Defaults(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := filterTools(reg, "read,bash")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Get("read"); !ok {
		t.Error("read dropped")
	}
	if _, ok := out.Get("bash"); !ok {
		t.Error("bash dropped")
	}
	if _, ok := out.Get("edit"); ok {
		t.Error("edit not filtered")
	}
}

func TestFilterTools_unknownErrors(t *testing.T) {
	reg, err := buildTools(t.TempDir(), config.Defaults(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = filterTools(reg, "read,bogus")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error missing name: %v", err)
	}
}

func TestFilterTools_emptyPassthrough(t *testing.T) {
	reg, err := buildTools(t.TempDir(), config.Defaults(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := filterTools(reg, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != reg {
		t.Error("empty csv should pass through")
	}
}

// buildTools must register the canonical tool vocabulary so the names are
// dispatchable.
func TestBuildTools_CanonicalNames(t *testing.T) {
	reg, err := buildTools(t.TempDir(), config.Defaults(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bash", "read", "write", "edit", "search", "glob", "ls"} {
		if _, ok := reg.Get(want); !ok {
			t.Errorf("registry missing canonical tool %q", want)
		}
	}
	// apply_patch + hashline_edit are registered too.
	for _, extra := range []string{"apply_patch", "hashline_edit"} {
		if _, ok := reg.Get(extra); !ok {
			t.Errorf("registry missing extra %q ", extra)
		}
	}
}

// apply_patch is registered by default.
func TestBuildTools_RegistersApplyPatch(t *testing.T) {
	reg, err := buildTools(t.TempDir(), config.Defaults(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("apply_patch"); !ok {
		t.Error("apply_patch must be registered")
	}
}

// DisabledTools removes named tools from the registry at build time.
func TestBuildTools_DisabledToolsDropped(t *testing.T) {
	cfg := config.Defaults()
	cfg.DisabledTools = []string{"write"}
	reg, err := buildTools(t.TempDir(), cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("write"); ok {
		t.Error("write should be excluded when listed in disabled_tools")
	}
	if _, ok := reg.Get("read"); !ok {
		t.Error("read should still be present")
	}
}

// UserTools are registered as dispatchable tools alongside builtins.
func TestBuildTools_UserToolsRegistered(t *testing.T) {
	cfg := config.Defaults()
	cfg.UserTools = []config.UserTool{{Name: "lint", Command: "true", Description: "run lint"}}
	reg, err := buildTools(t.TempDir(), cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	t1, ok := reg.Get("lint")
	if !ok {
		t.Fatal("user tool 'lint' missing from registry")
	}
	if t1.Spec().Name != "lint" {
		t.Errorf("user tool name lost: %q", t1.Spec().Name)
	}
}

// withWriteFilter restricts the mutation tools to matching paths; the other
// tool surface is untouched.
func TestBuildTools_WriteFilterRestrictsWrites(t *testing.T) {
	dir := t.TempDir()
	// confined scope roots the write tool at dir; unconfined would resolve
	// relative paths against the test process cwd.
	cfg := config.Defaults()
	cfg.Sandbox.Scope = "workspace-write"
	reg, err := buildToolsWithApprover(dir, cfg, nil, "", nil, withWriteFilter(regexp.MustCompile(`\.go$`)))
	if err != nil {
		t.Fatal(err)
	}
	w, ok := reg.Get("write")
	if !ok {
		t.Fatal("write missing")
	}
	res, err := w.Run(context.Background(), map[string]any{"path": "notes.txt", "content": "x"})
	if err != nil || !res.IsError {
		t.Fatalf("write outside filter = (%+v, %v), want refusal", res, err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "notes.txt")); serr == nil {
		t.Error("filtered write touched the file")
	}
	res, err = w.Run(context.Background(), map[string]any{"path": "ok.go", "content": "package x\n"})
	if err != nil || res.IsError {
		t.Fatalf("write inside filter = (%+v, %v), want success", res, err)
	}
}
