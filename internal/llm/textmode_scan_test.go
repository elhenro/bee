package llm

import (
	"strings"
	"testing"
)

func TestHasCloseTagSince(t *testing.T) {
	tags := []string{"</shell>", "</write>"}
	cases := []struct {
		name string
		s    string
		from int
		want bool
	}{
		{"tag in new bytes", "abc</shell>", 3, true},
		{"tag split across the boundary", "abc</sh" + "ell>", 7, true},
		{"old tag is not rescanned", "x</shell>yyy", 9, false},
		{"tag ends exactly at from", "x</shell>", 9, false},
		{"nothing new", "abc", 3, false},
		{"empty", "", 0, false},
	}
	for _, c := range cases {
		if got := hasCloseTagSince(c.s, c.from, tags); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// the call must dispatch once however the stream is chunked, including cuts
// through the close tag, and surrounding prose must survive.
func TestTextMode_CallDispatchesAtEveryChunkSize(t *testing.T) {
	src := `before <shell>{"command":"ls"}</shell> mid <write>{"path":"a","content":"b"}</write> after`
	for size := 1; size <= 9; size++ {
		var deltas []string
		for i := 0; i < len(src); i += size {
			deltas = append(deltas, src[i:min(i+size, len(src))])
		}
		calls, _, text := runThink(t, deltas...)
		if strings.Join(calls, ",") != "shell,write" {
			t.Fatalf("size %d: calls = %v", size, calls)
		}
		if !strings.Contains(text, "before") || !strings.Contains(text, "after") {
			t.Errorf("size %d: prose lost: %q", size, text)
		}
	}
}

// a close tag the extractor cannot consume (it sits inside an open call's
// string) must not stall later calls, and a long stream after it must stay
// correct.
func TestTextMode_UnconsumedCloseTagThenCall(t *testing.T) {
	deltas := []string{`<shell>{"command":"echo </shell>`, `"}`, `</shell>`}
	for range 2000 {
		deltas = append(deltas, " filler")
	}
	deltas = append(deltas, ` <write>{"path":"a","content":"b"}</write>`)
	calls, _, _ := runThink(t, deltas...)
	if strings.Join(calls, ",") != "shell,write" {
		t.Fatalf("calls = %v", calls)
	}
}
