package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectChrome_HonorsOverride(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "chrome")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DetectChrome(fake)
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Errorf("override ignored: got %q want %q", got, fake)
	}
}

func TestDetectChrome_OverrideMissing(t *testing.T) {
	if _, err := DetectChrome("/no/such/chrome"); err == nil {
		t.Error("expected error for missing override path")
	}
}

func TestDetectChrome_NoOverrideReturnsResult(t *testing.T) {
	_, err := DetectChrome("")
	if err != nil && err != ErrNotFound {
		t.Errorf("unexpected error type: %v", err)
	}
}
