package beehome

import (
	"path/filepath"
	"testing"
)

func TestDirHonorsBeeHome(t *testing.T) {
	t.Setenv("BEE_HOME", "/tmp/iso")
	if d, _ := Dir(); d != "/tmp/iso" {
		t.Fatalf("Dir = %q", d)
	}
	if p, _ := Join("auth"); p != filepath.Join("/tmp/iso", "auth") {
		t.Fatalf("Join = %q", p)
	}
}

func TestDirDefaultsUnderHome(t *testing.T) {
	t.Setenv("BEE_HOME", "")
	t.Setenv("HOME", "/tmp/h")
	if d, _ := Dir(); d != filepath.Join("/tmp/h", ".bee") {
		t.Fatalf("Dir = %q", d)
	}
}
