package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripRedundantCd_MatchesCwd(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := "cd " + cwd + " && go build ./..."
	out, note := stripRedundantCd(cmd)
	if out != "go build ./..." {
		t.Errorf("expected stripped command, got %q", out)
	}
	if !strings.Contains(note, "stripped redundant") {
		t.Errorf("expected note, got %q", note)
	}
}

func TestStripRedundantCd_DifferentDir(t *testing.T) {
	// /tmp != cwd, so leave intact.
	cmd := "cd /tmp && ls"
	out, note := stripRedundantCd(cmd)
	if out != cmd {
		t.Errorf("expected unchanged, got %q", out)
	}
	if note != "" {
		t.Errorf("expected no note, got %q", note)
	}
}

func TestStripRedundantCd_NoCdPrefix(t *testing.T) {
	cmd := "go test ./..."
	out, note := stripRedundantCd(cmd)
	if out != cmd || note != "" {
		t.Errorf("expected unchanged + no note, got %q / %q", out, note)
	}
}

func TestStripRedundantCd_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// fake home as cwd for this test.
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(home); err != nil {
		t.Skip("cannot chdir to home")
	}
	cmd := "cd ~ && ls"
	out, note := stripRedundantCd(cmd)
	if out != "ls" {
		t.Errorf("expected `ls`, got %q", out)
	}
	if note == "" {
		t.Error("expected note for tilde-expanded cwd")
	}
}

func TestStripRedundantCd_QuotedPath(t *testing.T) {
	cwd, _ := os.Getwd()
	cmd := `cd "` + cwd + `" && pwd`
	out, _ := stripRedundantCd(cmd)
	if out != "pwd" {
		t.Errorf("expected `pwd`, got %q", out)
	}
}

func TestStripRedundantCd_Semicolon(t *testing.T) {
	cwd, _ := os.Getwd()
	cmd := "cd " + cwd + "; pwd"
	out, _ := stripRedundantCd(cmd)
	if out != "pwd" {
		t.Errorf("expected `pwd`, got %q", out)
	}
}

func TestStripRedundantCd_NestedCd(t *testing.T) {
	// `cd a && cd b && c` — strip outer only if outer matches cwd. Inner cd
	// is intentional and stays.
	cwd, _ := os.Getwd()
	other := filepath.Join(os.TempDir(), "_not_cwd_subdir")
	cmd := "cd " + cwd + " && cd " + other + " && ls"
	out, note := stripRedundantCd(cmd)
	if out != "cd "+other+" && ls" {
		t.Errorf("expected outer strip only, got %q", out)
	}
	if note == "" {
		t.Error("expected note")
	}
}

func TestStripRedundantCd_RelativePathKept(t *testing.T) {
	// relative `cd foo` — can't know without joining. Leave as-is.
	cmd := "cd foo && ls"
	out, note := stripRedundantCd(cmd)
	if out != cmd {
		t.Errorf("expected unchanged for relative cd, got %q", out)
	}
	if note != "" {
		t.Errorf("expected no note, got %q", note)
	}
}

func TestStripRedundantCd_DynamicTargetKept(t *testing.T) {
	// $VAR or `cmd` substitutions are dynamic — don't strip.
	for _, cmd := range []string{
		"cd $PROJECT && ls",
		"cd `pwd` && ls",
		"cd ${HOME} && ls",
	} {
		out, _ := stripRedundantCd(cmd)
		if out != cmd {
			t.Errorf("expected unchanged for dynamic %q, got %q", cmd, out)
		}
	}
}
