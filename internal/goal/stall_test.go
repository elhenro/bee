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
