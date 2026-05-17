package main

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func tinyCfg() config.Config {
	c := config.Defaults()
	c.Profile = "tiny"
	return c
}

func normalCfg() config.Config {
	c := config.Defaults()
	c.Profile = "normal"
	return c
}

func TestFilterTools_subsetKeepsNamed(t *testing.T) {
	reg, err := buildTools(t.TempDir(), normalCfg(), nil, "")
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
	reg, err := buildTools(t.TempDir(), normalCfg(), nil, "")
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
	reg, err := buildTools(t.TempDir(), normalCfg(), nil, "")
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
// dispatchable. Profile filtering trims what the *model* sees; the registry
// itself stays complete for non-tiny profiles.
func TestBuildTools_CanonicalNames(t *testing.T) {
	reg, err := buildTools(t.TempDir(), normalCfg(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bash", "read", "write", "edit", "grep", "find", "ls"} {
		if _, ok := reg.Get(want); !ok {
			t.Errorf("registry missing canonical tool %q", want)
		}
	}
	// non-tiny keeps apply_patch + hashline_edit.
	for _, extra := range []string{"apply_patch", "hashline_edit"} {
		if _, ok := reg.Get(extra); !ok {
			t.Errorf("registry missing extra %q (must be registered for opt-in dispatch)", extra)
		}
	}
}

// tiny profile drops apply_patch — small models mis-emit unified diffs.
func TestBuildTools_TinySkipsApplyPatch(t *testing.T) {
	reg, err := buildTools(t.TempDir(), tinyCfg(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("apply_patch"); ok {
		t.Error("tiny profile must not register apply_patch")
	}
	// other mutators stay.
	for _, want := range []string{"write", "edit", "hashline_edit"} {
		if _, ok := reg.Get(want); !ok {
			t.Errorf("tiny profile missing %q (only apply_patch should be dropped)", want)
		}
	}
}

// normal keeps apply_patch in the registry — required for opt-in dispatch.
func TestBuildTools_NormalKeepsApplyPatch(t *testing.T) {
	reg, err := buildTools(t.TempDir(), normalCfg(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("apply_patch"); !ok {
		t.Error("normal profile must register apply_patch")
	}
}
