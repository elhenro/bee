//go:build !windows

package read

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// a fifo named like a picture reports size 0; it must be refused, not read
func TestReadImageRefusesNonRegularFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pipe.png")
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	res, _ := New().Run(context.Background(), map[string]any{"path": p})
	if !res.IsError || len(res.Images) != 0 || !strings.Contains(res.Content, "not a regular file") {
		t.Fatalf("want regular-file refusal, got %+v", res)
	}
}
