package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

const calcMD = `---
name: calc
type: prompt
description: stage-and-commit
---
You are stage-then-commit.`

const hermesMD = `---
name: hermes
type: exec
description: daily-driver agent
exec: ["echo", "ok"]
---
`

const brokenMD = `not even yaml`

func TestRegistry_LoadAndManifest(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "calc.md"), calcMD)
	mustWrite(t, filepath.Join(dir, "hermes.md"), hermesMD)
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "noise")

	r := NewRegistry()
	if err := r.Load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}

	if _, ok := r.Get("calc"); !ok {
		t.Fatal("calc missing")
	}
	if _, ok := r.Get("hermes"); !ok {
		t.Fatal("hermes missing")
	}
	if got := r.List(); len(got) != 2 {
		t.Fatalf("list len=%d", len(got))
	}
	m := r.Manifest()
	if !strings.Contains(m, "calc:") || !strings.Contains(m, "hermes:") {
		t.Fatalf("manifest missing entries: %q", m)
	}
	// manifest is one-line-per-skill
	if strings.Count(m, "\n") != 1 {
		t.Fatalf("expected 2 lines (1 newline), got: %q", m)
	}
}

func TestRegistry_LoadCollectsParseErrors(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "ok.md"), calcMD)
	mustWrite(t, filepath.Join(dir, "bad.md"), brokenMD)

	r := NewRegistry()
	err := r.Load(dir)
	if err == nil {
		t.Fatal("want parse error")
	}
	// the valid skill must still be loaded
	if _, ok := r.Get("calc"); !ok {
		t.Fatal("calc should load despite sibling failure")
	}
}

func TestRegistry_UpsertAndRemove(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "calc.md")
	mustWrite(t, p, calcMD)

	r := NewRegistry()
	if _, err := r.Upsert(p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, ok := r.Get("calc"); !ok {
		t.Fatal("not stored")
	}
	r.RemovePath(p)
	if _, ok := r.Get("calc"); ok {
		t.Fatal("still present after remove")
	}
}

func TestRegistry_LoadMissingDir(t *testing.T) {
	r := NewRegistry()
	if err := r.Load("/nonexistent/path/definitely/not/here"); err != nil {
		t.Fatalf("missing dir should be soft-error: %v", err)
	}
	if len(r.List()) != 0 {
		t.Fatal("expected empty registry")
	}
}

func TestRegistry_ManifestEmpty(t *testing.T) {
	r := NewRegistry()
	if m := r.Manifest(); m != "" {
		t.Fatalf("empty registry manifest should be empty, got %q", m)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := writeFileBytes(p, []byte(body)); err != nil {
		t.Fatal(err)
	}
}
