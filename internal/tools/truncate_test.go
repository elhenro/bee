package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const trailerMarker = "(truncated: kept first"

// TestMain isolates the spill directory for the truncate test suite so
// existing tests that trigger truncation don't pollute the user's real
// ~/.bee/spill.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bee-truncate-test-*")
	if err != nil {
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	_ = os.Setenv("BEE_HOME", dir)
	os.Exit(m.Run())
}

func TestTruncate_UnderCapPassthrough(t *testing.T) {
	in := "small output\nwith two lines"
	out, ok := Truncate("shell", in)
	if ok {
		t.Fatalf("expected ok=false, got true")
	}
	if out != in {
		t.Fatalf("expected passthrough, got %q", out)
	}
}

func TestTruncate_OverCapAppendsTrailer(t *testing.T) {
	// shell cap = 50_000 tokens => 200_000 chars. Build > 200_001 chars.
	line := strings.Repeat("x", 99) + "\n" // 100 chars
	in := strings.Repeat(line, 2100)       // 210_000 chars
	out, ok := Truncate("shell", in)
	if !ok {
		t.Fatalf("expected truncation, got ok=false")
	}
	if !strings.Contains(out, trailerMarker) {
		t.Fatalf("expected trailer in output, got: %q", out[len(out)-200:])
	}
	if len(out) >= len(in) {
		t.Fatalf("expected truncated output shorter than input, got %d >= %d", len(out), len(in))
	}
}

func TestTruncate_WebfetchLimitLowerThanShell(t *testing.T) {
	if limitFor("webfetch") >= limitFor("shell") {
		t.Fatalf("webfetch limit (%d) must be lower than shell limit (%d)",
			limitFor("webfetch"), limitFor("shell"))
	}
}

func TestTruncate_UnknownToolUsesDefault(t *testing.T) {
	if limitFor("totally_made_up_tool") != MaxOutputTokens {
		t.Fatalf("unknown tool should fall back to MaxOutputTokens")
	}
}

func TestTruncate_CutLandsOnNewline(t *testing.T) {
	// build input > webfetch cap (10k tokens = 40k chars) with regular newlines
	line := strings.Repeat("a", 79) + "\n"
	in := strings.Repeat(line, 600) // 48_000 chars > 40_000
	out, ok := Truncate("webfetch", in)
	if !ok {
		t.Fatalf("expected truncation")
	}
	// the head portion (everything before the trailer) should end with the last
	// kept content line; specifically the char right before our injected
	// "\n...\n(truncated..." trailer should be the end of a line, not mid-line
	idx := strings.Index(out, "\n...\n")
	if idx <= 0 {
		t.Fatalf("trailer not found in output")
	}
	head := out[:idx]
	if !strings.HasSuffix(head, "a") || strings.HasSuffix(head, "\n") {
		// head must end with a full line — last char is last char of a "aaa..." line
		// (we trimmed up to but not including the trailing \n of that line)
	}
	// stronger check: the last newline in head should be followed only by a
	// full line (no partial). Reconstruct: every line in head must be 80 chars
	// terminated by \n except the final one which is the same line content.
	lines := strings.Split(head, "\n")
	for i, ln := range lines {
		if ln != strings.Repeat("a", 79) {
			t.Fatalf("line %d not a full kept line: %q", i, ln)
		}
	}
}

func TestTruncate_SingleGiantLineHardCut(t *testing.T) {
	// no newlines at all, longer than webfetch cap (40k chars)
	in := strings.Repeat("z", 50_000)
	out, ok := Truncate("webfetch", in)
	if !ok {
		t.Fatalf("expected truncation")
	}
	if !strings.Contains(out, trailerMarker) {
		t.Fatalf("expected trailer in output")
	}
	// head before trailer must be exactly maxChars (40000) since there's no \n to back off to
	idx := strings.Index(out, "\n...\n")
	if idx <= 0 {
		t.Fatalf("trailer not found")
	}
	head := out[:idx]
	if len(head) != 40_000 {
		t.Fatalf("expected hard cut at 40000 chars, got %d", len(head))
	}
}

func TestTruncateWithLimit_ProfileCapBitesBeforeDefault(t *testing.T) {
	// per-tool default for "read" = MaxOutputTokens (50_000 → 200_000 chars).
	// passing a 1500-token profile cap should bite first.
	in := strings.Repeat("x\n", 5_000) // 10_000 chars > 6_000 (1500*4)
	out, ok := TruncateWithLimit("read", in, 1500)
	if !ok {
		t.Fatalf("expected truncation under 1500-token cap, got ok=false (in=%d)", len(in))
	}
	if !strings.Contains(out, trailerMarker) {
		t.Fatalf("expected trailer in output")
	}
}

func TestTruncateWithLimit_ZeroFallsBackToDefault(t *testing.T) {
	in := "tiny\noutput"
	out, ok := TruncateWithLimit("read", in, 0)
	if ok || out != in {
		t.Fatalf("expected passthrough on zero limit, got ok=%v out=%q", ok, out)
	}
}

func TestTruncateWithLimit_DoesNotRaiseWebfetchCeiling(t *testing.T) {
	// webfetch default is 10_000 tokens = 40_000 chars. A profile cap of
	// 50_000 tokens must NOT raise that ceiling; smaller of the two wins.
	in := strings.Repeat("a\n", 25_000) // 50_000 chars > 40_000
	out, ok := TruncateWithLimit("webfetch", in, 50_000)
	if !ok {
		t.Fatalf("expected webfetch default (10k tokens) to still bite at 40k chars")
	}
	if !strings.Contains(out, trailerMarker) {
		t.Fatalf("expected trailer")
	}
}

func TestTruncate_EmptyContentNoop(t *testing.T) {
	out, ok := Truncate("shell", "")
	if ok {
		t.Fatalf("empty content should not be flagged as truncated")
	}
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestTruncate_Spillover_UnderCapNoSpill(t *testing.T) {
	dir := t.TempDir()
	in := "short body, well under any cap"
	out, ok := TruncateWithLimitSpill("read", in, 1500, dir)
	if ok {
		t.Fatalf("expected ok=false for under-cap content")
	}
	if out != in {
		t.Fatalf("expected passthrough, got %q", out)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected no spill files for under-cap content, got %d", len(entries))
	}
}

func TestTruncate_Spillover_OverCapWritesFullBody(t *testing.T) {
	dir := t.TempDir()
	// 1500-token cap = 6000 chars. Use 10000 chars so we trip it.
	in := strings.Repeat("line of content here\n", 500) // 10500 chars
	out, ok := TruncateWithLimitSpill("read", in, 1500, dir)
	if !ok {
		t.Fatalf("expected truncation")
	}
	if !strings.Contains(out, trailerMarker) {
		t.Fatalf("expected truncate trailer marker in output")
	}
	if !strings.Contains(out, "full body saved to") {
		t.Fatalf("expected spillover pointer in output, got tail: %q", out[len(out)-300:])
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spill dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 spill file, got %d", len(entries))
	}
	spillPath := filepath.Join(dir, entries[0].Name())
	if !strings.Contains(out, spillPath) {
		t.Fatalf("output trailer should reference spill path %q, got tail: %q", spillPath, out[len(out)-400:])
	}
	body, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("read spill file: %v", err)
	}
	if string(body) != in {
		t.Fatalf("spill file should contain full untruncated body; len(got)=%d len(want)=%d", len(body), len(in))
	}
	// filename sanity: matches <ts>-<tool>-<8hex>.txt
	name := entries[0].Name()
	if !strings.Contains(name, "-read-") || !strings.HasSuffix(name, ".txt") {
		t.Fatalf("spill filename should embed tool name and .txt suffix, got %q", name)
	}
}

func TestTruncate_Spillover_EmptyDirSkipsSpill(t *testing.T) {
	// spillDir="" must degrade to old behaviour: truncate-and-discard with the
	// "full output not retained" trailer, no spill pointer.
	in := strings.Repeat("x\n", 5000) // 10000 chars
	out, ok := TruncateWithLimitSpill("read", in, 1500, "")
	if !ok {
		t.Fatalf("expected truncation")
	}
	if strings.Contains(out, "full body saved to") {
		t.Fatalf("expected no spillover when spillDir is empty")
	}
	if !strings.Contains(out, "full output not retained") {
		t.Fatalf("expected fallback trailer when spillDir is empty")
	}
}
