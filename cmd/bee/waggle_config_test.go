package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr runs fn and returns what it wrote to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = old
	_ = w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

func writeBeeConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("BEE_HOME", home)
	t.Setenv("BEE_CONFIG", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// a missing provider key must not discard the user's sandbox scope: an exec
// waggle would otherwise run unconfined.
func TestLoadConfigNoKeyKeepsSandbox(t *testing.T) {
	writeBeeConfig(t, "[sandbox]\nscope = \"workspace-write\"\n")
	var scope string
	if out := captureStderr(t, func() { scope = loadConfigNoKey("bee x").Sandbox.Scope }); out != "" {
		t.Errorf("missing key should be silent, got %q", out)
	}
	if scope != "workspace-write" {
		t.Fatalf("sandbox scope = %q, want workspace-write", scope)
	}
}

// a config that fails to parse is reported instead of silently replaced.
func TestLoadConfigNoKeyReportsParseError(t *testing.T) {
	writeBeeConfig(t, "[sandbox]\nscope = \n")
	out := captureStderr(t, func() { loadConfigNoKey("bee x") })
	if !strings.Contains(out, "bee x:") || !strings.Contains(out, "config.toml") {
		t.Fatalf("parse error not reported on stderr: %q", out)
	}
}

// a malformed skill is reported, and the valid ones around it still load.
func TestNewSkillRegistryReportsMalformedSkill(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_SKILLS_DIR", dir)
	good := "---\nname: good\ntype: prompt\ndescription: fine\n---\nbody\n"
	for name, body := range map[string]string{"good.md": good, "broken.md": "no frontmatter at all"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var ok bool
	out := captureStderr(t, func() {
		_, ok = newSkillRegistry().Get("good")
	})
	if !ok {
		t.Error("valid skill lost next to a malformed one")
	}
	if !strings.Contains(out, "broken.md") {
		t.Errorf("malformed skill not reported on stderr: %q", out)
	}
}
