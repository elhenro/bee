package tui

import "testing"

// every painter name round-trips to the style whose painter it is
func TestParseLoaderStyle_PainterNames(t *testing.T) {
	for i, p := range brailleNamedPainters {
		want := LoaderStyle(i + 1)
		if got := ParseLoaderStyle(p.name); got != want {
			t.Errorf("ParseLoaderStyle(%q) = %v, want %v", p.name, got, want)
		}
		r := &StreamRenderer{loaderStyle: want}
		if got, w := r.painterFor(0)(3, 12), p.fn(3, 12); got != w {
			t.Errorf("painterFor(%q) rendered %q, want %q", p.name, got, w)
		}
	}
	if got := ParseLoaderStyle("wagglerun"); got != LoaderStyleWaggleRun {
		t.Errorf("wagglerun = %v, want %v", got, LoaderStyleWaggleRun)
	}
}

// random draws from the painter list: it reaches the last one and never lands
// on the phased or strip pseudo-styles
func TestParseLoaderStyle_RandomCoversPainters(t *testing.T) {
	seen := map[LoaderStyle]bool{}
	for range 3000 {
		s := ParseLoaderStyle("random")
		if s == LoaderStylePhased || s == LoaderStyleStrip {
			t.Fatalf("random picked pseudo-style %v", s)
		}
		seen[s] = true
	}
	if !seen[LoaderStyleWaggleRun] {
		t.Error("random never picked the last painter")
	}
}
