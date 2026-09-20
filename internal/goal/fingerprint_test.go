package goal

import "testing"

// TestReplyFingerprint covers the fingerprint contract: line-ending and outer
// whitespace variance is ignored, inner content drives the digest, empty
// replies produce no fingerprint, and the result is deterministic.
func TestReplyFingerprint(t *testing.T) {
	t.Run("line endings and outer whitespace normalize", func(t *testing.T) {
		base := "line one\nline two\nline three"
		variants := []string{
			base,
			"line one\r\nline two\r\nline three",
			"line one\rline two\rline three",
			"  \n" + base + "\n\t ",
			"\r\n" + base + "\r\n",
		}
		want := ReplyFingerprint(base)
		if want == "" {
			t.Fatal("baseline fingerprint should not be empty")
		}
		for _, v := range variants {
			if got := ReplyFingerprint(v); got != want {
				t.Errorf("ReplyFingerprint(%q) = %q, want %q", v, got, want)
			}
		}
	})

	t.Run("inner text changes the fingerprint", func(t *testing.T) {
		a := "the build is green\nall tests pass"
		b := "the build is green\nall tests failed"
		c := "the build is green\n all tests pass" // inner space preserved
		if ReplyFingerprint(a) == ReplyFingerprint(b) {
			t.Error("inner word change must change the fingerprint")
		}
		if ReplyFingerprint(a) == ReplyFingerprint(c) {
			t.Error("inner whitespace change must change the fingerprint")
		}
	})

	t.Run("empty and whitespace-only yield empty", func(t *testing.T) {
		for _, in := range []string{"", " ", "\t", "\n", "\r\n", " \r\n\t "} {
			if got := ReplyFingerprint(in); got != "" {
				t.Errorf("ReplyFingerprint(%q) = %q, want empty", in, got)
			}
		}
	})

	t.Run("deterministic across runs", func(t *testing.T) {
		reply := "same reply content"
		first := ReplyFingerprint(reply)
		for i := 0; i < 5; i++ {
			if got := ReplyFingerprint(reply); got != first {
				t.Fatalf("run %d: %q != %q", i, got, first)
			}
		}
	})
}

// TestReplyFingerprintTracker verifies per-thread repeat detection: the first
// reply is never a repeat, an identical follow-up is, threads stay isolated,
// and empty replies never register as progress-blocking repeats.
func TestReplyFingerprintTracker(t *testing.T) {
	var ft FingerprintTracker
	thread := "goal-1"

	if ft.Observe(thread, "step one done") {
		t.Fatal("first reply must not be a repeat")
	}
	if ft.Last(thread) == "" {
		t.Fatal("fingerprint should be stored after first reply")
	}
	fp := ft.Last(thread)

	if !ft.Observe(thread, "step one done\r\n") {
		t.Fatal("normalized repeat must be detected")
	}
	if ft.Last(thread) != fp {
		t.Fatal("repeat must keep the stored fingerprint")
	}

	if ft.Observe(thread, "step two done") {
		t.Fatal("changed reply must not be a repeat")
	}

	if ft.Observe("goal-2", "step two done") {
		t.Fatal("a different thread must not inherit another thread's fingerprint")
	}

	before := ft.Last(thread)
	if ft.Observe(thread, "   ") {
		t.Fatal("empty reply must not be a repeat")
	}
	if ft.Last(thread) != before {
		t.Fatal("empty reply must not overwrite the stored fingerprint")
	}

	if got := ft.Last("never-seen"); got != "" {
		t.Fatalf("unknown thread fingerprint = %q, want empty", got)
	}
}