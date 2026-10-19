package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllBundledSkillsParse(t *testing.T) {
	files, err := filepath.Glob("bundled/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(f, b); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
