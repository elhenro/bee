package godoc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// helper: skip if `go` binary unavailable in CI sandbox.
func requireGo(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/opt/homebrew/bin/go"); err == nil {
		return
	}
	if _, err := os.Stat("/usr/local/go/bin/go"); err == nil {
		return
	}
	if _, err := os.Stat("/usr/bin/go"); err == nil {
		return
	}
	t.Skip("go binary not found")
}

func TestGodoc_Stdlib(t *testing.T) {
	requireGo(t)
	tool := New(".")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := tool.Run(ctx, map[string]any{"target": "strings.TrimSpace"})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "TrimSpace") {
		t.Errorf("expected docs for TrimSpace, got: %s", res.Content)
	}
}

func TestGodoc_PhantomAPI(t *testing.T) {
	// the session that motivated this tool tried to call
	// transform.TrimSpace which does not exist. confirm godoc returns the
	// real surface so the model sees Append/Bytes/String etc.
	requireGo(t)
	tool := New(".")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := tool.Run(ctx, map[string]any{"target": "golang.org/x/text/transform"})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if res.IsError {
		t.Skipf("module not in local cache (expected on CI): %s", res.Content)
	}
	if strings.Contains(res.Content, "TrimSpace") {
		t.Errorf("unexpected: transform package contains TrimSpace? %s", res.Content)
	}
	if !strings.Contains(res.Content, "Transformer") {
		t.Errorf("expected Transformer in docs, got: %s", res.Content)
	}
}

func TestGodoc_UnknownPkg(t *testing.T) {
	requireGo(t)
	tool := New(".")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := tool.Run(ctx, map[string]any{"target": "this/pkg/does/not/exist"})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError for missing pkg, got: %s", res.Content)
	}
}

func TestGodoc_MissingTarget(t *testing.T) {
	tool := New(".")
	res, err := tool.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if !res.IsError {
		t.Error("expected error on missing target")
	}
	if !strings.Contains(res.Content, "missing or empty") {
		t.Errorf("expected missing-field msg, got: %s", res.Content)
	}
}

func TestGodoc_RejectShellMeta(t *testing.T) {
	tool := New(".")
	for _, bad := range []string{"fmt; rm -rf /", "fmt|cat", "$EVIL", "-help"} {
		res, _ := tool.Run(context.Background(), map[string]any{"target": bad})
		if !res.IsError {
			t.Errorf("expected reject for %q, got success: %s", bad, res.Content)
		}
	}
}
