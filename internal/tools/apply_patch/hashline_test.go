package apply_patch

import (
	"strings"
	"testing"
)

func TestTagDeterministic(t *testing.T) {
	a := Tag("hello world", 1)
	b := Tag("hello world", 1)
	if a != b {
		t.Fatalf("expected same tag, got %q vs %q", a, b)
	}
}

func TestTagAlnumSeedIgnored(t *testing.T) {
	a := Tag("hello world", 1)
	b := Tag("hello world", 42)
	if a != b {
		t.Fatalf("alphanumeric line should ignore lineNumber: %q vs %q", a, b)
	}
}

func TestTagBlankLineVariesByNumber(t *testing.T) {
	// blank-line tags fold xxh32 into a 256-bucket space, so adjacent
	// seeds may occasionally collide. assert that *some* variation
	// exists across a small range, which proves the seed is in play.
	first := Tag("", 1)
	varied := false
	for i := 2; i <= 20; i++ {
		if Tag("", i) != first {
			varied = true
			break
		}
	}
	if !varied {
		t.Fatalf("blank line tags did not vary across lines 1..20; seed unused?")
	}
}

func TestTagPunctuationVariesByNumber(t *testing.T) {
	a := Tag("---", 1)
	b := Tag("---", 99)
	if a == b {
		t.Fatalf("punctuation-only line should vary by lineNumber, both were %q", a)
	}
}

func TestTagTrailingWhitespaceIgnored(t *testing.T) {
	a := Tag("foo bar", 7)
	b := Tag("foo bar   \t  ", 7)
	if a != b {
		t.Fatalf("trailing whitespace should normalize: %q vs %q", a, b)
	}
}

func TestTagCRStripped(t *testing.T) {
	a := Tag("foo bar", 3)
	b := Tag("foo bar\r", 3)
	c := Tag("\rfoo\r bar", 3)
	if a != b || a != c {
		t.Fatalf("CR should be stripped: %q %q %q", a, b, c)
	}
}

func TestTagShapeAndAlphabet(t *testing.T) {
	cases := []struct {
		line string
		ln   int
	}{
		{"package main", 1},
		{"", 2},
		{"\t\treturn nil", 10},
		{"---", 5},
		{"日本語のテスト", 11},
		{"   ", 12},
	}
	for _, c := range cases {
		got := Tag(c.line, c.ln)
		if len(got) != 2 {
			t.Fatalf("Tag(%q,%d) = %q, want len 2", c.line, c.ln, got)
		}
		for _, r := range got {
			if !strings.ContainsRune(hashAlphabet, r) {
				t.Fatalf("Tag(%q,%d) = %q has rune %q not in alphabet", c.line, c.ln, got, r)
			}
		}
	}
}

func TestTagAllLength(t *testing.T) {
	src := "one\ntwo\nthree\nfour\nfive"
	tags := TagAll(src)
	if len(tags) != 5 {
		t.Fatalf("want 5 tags, got %d", len(tags))
	}
	for i, tg := range tags {
		if len(tg) != 2 {
			t.Fatalf("tag %d = %q, want len 2", i, tg)
		}
	}
}

func TestTagAllMatchesTag(t *testing.T) {
	src := "alpha\nbeta\n\n  \nzeta"
	tags := TagAll(src)
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		want := Tag(l, i+1)
		if tags[i] != want {
			t.Fatalf("line %d: TagAll=%q Tag=%q", i+1, tags[i], want)
		}
	}
}

func TestRefFormat(t *testing.T) {
	r := Ref("hello world", 42)
	if !strings.HasPrefix(r, "42#") {
		t.Fatalf("Ref prefix wrong: %q", r)
	}
	if len(r) != len("42#")+2 {
		t.Fatalf("Ref length wrong: %q", r)
	}
	tag := Tag("hello world", 42)
	if r != "42#"+tag {
		t.Fatalf("Ref = %q, want %q", r, "42#"+tag)
	}
}

func TestTagLongLineNoPanic(t *testing.T) {
	// >16 bytes exercises the 4-lane main loop in xxh32.
	long := strings.Repeat("abcdefgh", 8)
	got := Tag(long, 1)
	if len(got) != 2 {
		t.Fatalf("long line tag len = %d", len(got))
	}
}
