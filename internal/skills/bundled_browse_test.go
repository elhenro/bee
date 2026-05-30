package skills

import (
	"os"
	"testing"
)

func TestBundledBrowseParses(t *testing.T) {
	b, err := os.ReadFile("bundled/browse.md")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Parse("bundled/browse.md", b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Kind != KindRecipe {
		t.Errorf("kind = %q", s.Kind)
	}
}
