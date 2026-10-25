package goal

import "testing"

func TestReplyStreakStopsOnThirdIdentical(t *testing.T) {
	var r ReplyStreak
	for i, want := range []bool{false, false, true} {
		if got := r.Observe("still working on it\r\n"); got != want {
			t.Fatalf("observe #%d = %v, want %v", i+1, got, want)
		}
	}
	if r.Count() != MaxIdenticalReplies {
		t.Fatalf("count = %d, want %d", r.Count(), MaxIdenticalReplies)
	}
}

func TestReplyStreakResetsOnChangedReply(t *testing.T) {
	var r ReplyStreak
	r.Observe("a")
	r.Observe("a")
	if r.Observe("b") {
		t.Fatal("changed reply must not stall")
	}
	if r.Count() != 1 {
		t.Fatalf("count after change = %d, want 1", r.Count())
	}
}

func TestReplyStreakIgnoresEmpty(t *testing.T) {
	var r ReplyStreak
	r.Observe("a")
	r.Observe("a")
	if r.Observe("  \n") {
		t.Fatal("empty reply must not stall")
	}
	if !r.Observe("a") {
		t.Fatal("empty reply must not reset the streak")
	}
}

// line-ending and outer whitespace variance is ignored; inner content counts.
func TestReplyStreakNormalizesReplies(t *testing.T) {
	base := "line one\nline two\nline three"
	for _, v := range []string{
		"line one\r\nline two\r\nline three",
		"line one\rline two\rline three",
		"  \n" + base + "\n\t ",
		"\r\n" + base + "\r\n",
	} {
		var r ReplyStreak
		r.Observe(base)
		r.Observe(v)
		if r.Count() != 2 {
			t.Errorf("variant %q count = %d, want 2", v, r.Count())
		}
	}
	for _, v := range []string{
		"line one\nline two\nline four",
		"line one\n line two\nline three", // inner space preserved
	} {
		var r ReplyStreak
		r.Observe(base)
		r.Observe(v)
		if r.Count() != 1 {
			t.Errorf("variant %q count = %d, want 1", v, r.Count())
		}
	}
}
