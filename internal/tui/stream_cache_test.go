package tui

import (
	"fmt"
	"strings"
	"testing"
)

// rows built from a growing buffer, delta by delta, equal rows built in one go
func TestThoughtRows_IncrementalMatchesFreshRender(t *testing.T) {
	var sb strings.Builder
	for i := range 12 {
		fmt.Fprintf(&sb, "step %d: %s\n\n", i, strings.Repeat("reasoning about the handler ", 1+i%4))
	}
	sb.WriteString("tail without newline\ttab")
	text := sb.String()

	for _, width := range []int{30, 80} {
		live := NewStreamRenderer(DefaultStyles(), width)
		for n := 0; n <= len(text); n += 7 {
			got := live.thoughtRows(text[:n])
			want := NewStreamRenderer(DefaultStyles(), width).thoughtRows(text[:n])
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("width %d, %d bytes: incremental rows differ\n got %q\nwant %q", width, n, got, want)
			}
		}
	}
}

func TestThoughtRows_SettledLinesAreNotRedone(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 60)
	r.thoughtRows("first line\nsecond line\npart")
	if want := "first line\nsecond line\n"; r.live.thoughtSrc != want {
		t.Fatalf("settled prefix: want %q, got %q", want, r.live.thoughtSrc)
	}
	r.live.thoughtRows[0] = "PLANTED"
	rows := r.thoughtRows("first line\nsecond line\npart of the third\n")
	if rows[0] != "PLANTED" || len(rows) != 3 {
		t.Fatalf("settled rows must come from the memo: %q", rows)
	}
	// a changed prefix or width starts over
	if rows = r.thoughtRows("other\n"); strings.Contains(strings.Join(rows, ""), "PLANTED") {
		t.Fatalf("memo survived a different buffer: %q", rows)
	}
}

func TestThoughtRows_EscapesTakeTheWholeTextPath(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 60)
	rows := r.thoughtRows("a \x1b[31mred\nline\x1b[0m\nnext\n")
	if got := stripANSI(strings.Join(rows, "\n")); strings.Contains(got, "[31m") || strings.Contains(got, "[0m") {
		t.Fatalf("escape sequence spanning a line leaked: %q", got)
	}
}

// a long chain of thought, one more delta per frame; cold is the cost of
// styling the whole buffer each frame
func BenchmarkRenderThinkingPartial(b *testing.B) {
	line := strings.Repeat("considering the next step of the plan ", 4) + "\n"
	think := strings.Repeat(line, 400)
	for _, cold := range []bool{false, true} {
		name := "settled"
		if cold {
			name = "cold"
		}
		b.Run(name, func(b *testing.B) {
			r := NewStreamRenderer(DefaultStyles(), 120)
			r.SetShowThoughts(true)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if cold {
					r.live = liveCache{}
				}
				_ = r.RenderThinkingPartial(think+"partial", 0, true)
			}
		})
	}
}
