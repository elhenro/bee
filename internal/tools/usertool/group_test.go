package usertool

import (
	"context"
	"strings"
	"testing"
	"time"
)

// a child that outlives bash and holds the pipe must not stall Run or turn a
// clean exit into an error.
func TestRun_BackgroundChildDoesNotStall(t *testing.T) {
	tool, err := New("bg", "sleep 4 & echo started", "d")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res, err := tool.Run(context.Background(), map[string]any{})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "started") {
		t.Fatalf("output missing: %q", res.Content)
	}
	if time.Since(start) > 3500*time.Millisecond {
		t.Fatalf("waited %s for the background child", time.Since(start))
	}
}

func TestRun_OutputCapped(t *testing.T) {
	tool, err := New("big", "head -c 100000 /dev/zero | tr '\\0' x", "d")
	if err != nil {
		t.Fatal(err)
	}
	res, _ := tool.Run(context.Background(), map[string]any{})
	if len(res.Content) > maxOutputBytes+100 || !strings.Contains(res.Content, "omitted") {
		t.Fatalf("output not capped: %d bytes", len(res.Content))
	}
}
