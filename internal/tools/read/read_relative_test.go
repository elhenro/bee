package read

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead_ConfinedRefusesRelativeProtectedPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".aws"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aws", "config"), []byte("[default]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	res, _ := NewWithLimits(0, 0, true).Run(context.Background(), map[string]any{"path": ".aws/config"})
	if !res.IsError || !strings.Contains(res.Content, "protected directory") {
		t.Errorf("confined relative read of .aws/config should be refused, got: %q", res.Content)
	}
	res, _ = NewWithLimits(0, 0, false).Run(context.Background(), map[string]any{"path": ".aws/config"})
	if res.IsError {
		t.Errorf("danger-full-access read must stay unrestricted, got: %q", res.Content)
	}
}
