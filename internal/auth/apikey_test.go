package auth

import (
	"path/filepath"
	"testing"
)

func TestAPIKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got, err := LoadAPIKey(dir, "omlx"); err != nil || got != "" {
		t.Fatalf("load empty = (%q,%v); want empty", got, err)
	}
	if err := SaveAPIKey(dir, "omlx", "sk-test-123\n"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadAPIKey(dir, "omlx")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "sk-test-123" {
		t.Errorf("load = %q; want %q", got, "sk-test-123")
	}
	if !HasAPIKey(dir, "omlx") {
		t.Error("HasAPIKey = false after save")
	}
	if err := DeleteAPIKey(dir, "omlx"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if HasAPIKey(dir, "omlx") {
		t.Error("HasAPIKey = true after delete")
	}
}

func TestSaveAPIKeyRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := SaveAPIKey(dir, "omlx", "   "); err == nil {
		t.Error("save with blank key should error")
	}
}

func TestDeleteAPIKeyMissingNoop(t *testing.T) {
	dir := t.TempDir()
	if err := DeleteAPIKey(dir, "omlx"); err != nil {
		t.Errorf("delete missing = %v; want nil", err)
	}
	// ensure no file created as side effect
	if _, err := filepath.Glob(filepath.Join(dir, "*.key")); err != nil {
		t.Fatalf("glob: %v", err)
	}
}
