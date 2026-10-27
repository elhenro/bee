package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateHeadTail_NoNewlineStillMarked(t *testing.T) {
	// one giant line: no newline in the tail window, so no tail can be shown.
	// the cut must still be announced to the model.
	in := strings.Repeat("世", 30_000) // 90k bytes, over the 4k-token cap
	out, ok := TruncateHeadTailWithLimit("", in, 4_000, -1)
	if !ok {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(out, trailerMarker) {
		t.Fatalf("no truncation marker in %q", out[len(out)-120:])
	}
	if !utf8.ValidString(out) {
		t.Fatal("output is not valid UTF-8")
	}
	if len(out) >= len(in) {
		t.Fatalf("output not shorter: %d >= %d", len(out), len(in))
	}
}

func TestTruncateHeadTail_KeepsTail(t *testing.T) {
	in := strings.Repeat("line of output\n", 5_000) + "FINAL PANIC\n"
	out, ok := TruncateHeadTailWithLimit("", in, 4_000, -1)
	if !ok {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(out, "(skipped") || !strings.HasSuffix(out, "FINAL PANIC\n") {
		t.Fatalf("head-tail shape lost: %q", out[len(out)-200:])
	}
}

func TestTruncateHeadTail_UnderCapPassthrough(t *testing.T) {
	out, ok := TruncateHeadTailWithLimit("", "short", 4_000, -1)
	if ok || out != "short" {
		t.Fatalf("got %q ok=%v", out, ok)
	}
}
