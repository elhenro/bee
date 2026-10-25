package safety

import (
	"os"
	"runtime"
	"testing"
)

func chdirTemp(t *testing.T) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestCheckReadable_RelativeProtectedDirs(t *testing.T) {
	chdirTemp(t)
	for _, p := range []string{
		".git/config", "./.git/config", ".aws/config", ".kube/config", ".ssh/config",
		".docker/config.json", ".config/gh/hosts.yml", "sub/.gnupg/pubring.kbx",
	} {
		if err := CheckReadable(p); err == nil {
			t.Errorf("CheckReadable(%q) = nil, want refusal", p)
		}
	}
	for _, p := range []string{"main.go", "internal/safety/paths.go", ".github/workflows/ci.yml", "docs/.gitkeep"} {
		if err := CheckReadable(p); err != nil {
			t.Errorf("CheckReadable(%q) = %v, want nil", p, err)
		}
	}
}

func TestCheckWritable_RelativeSystemDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("forbidden write prefixes are posix system dirs")
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := CheckWritable("etc/passwd"); err == nil {
		t.Error("CheckWritable(etc/passwd) from / = nil, want refusal")
	}
}
