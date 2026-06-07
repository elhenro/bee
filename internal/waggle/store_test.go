package waggle

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectStore_DeterministicPerRoot(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	a, err := ProjectStore("/some/project")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ProjectStore("/some/project")
	if a.Dir() != b.Dir() {
		t.Errorf("same root must map to same dir: %q vs %q", a.Dir(), b.Dir())
	}
	c, _ := ProjectStore("/other/project")
	if a.Dir() == c.Dir() {
		t.Error("different roots must map to different dirs")
	}
	if !strings.Contains(a.Dir(), filepath.Join("waggle", "proj")) {
		t.Errorf("unexpected project dir layout: %q", a.Dir())
	}
}

func TestUserStore_Path(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, err := UserStore()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Dir(), filepath.Join("waggle", "user")) {
		t.Errorf("unexpected user dir layout: %q", s.Dir())
	}
}

func TestStore_WriteAndExists(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	if s.Exists("w1") {
		t.Fatal("must not exist before write")
	}
	if err := s.Write("w1", "---\nname: w1\n---\nbody\n"); err != nil {
		t.Fatal(err)
	}
	if !s.Exists("w1") {
		t.Error("must exist after write")
	}
}
