package loop

import (
	"context"
	"errors"
	"testing"
)

func TestClassifyResume(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantResume bool
		wantReason string
	}{
		{"finished", nil, false, ""},
		{"user-cancel", context.Canceled, false, ""},
		{"escalate-sentinel", ErrEscalate, false, ""},
		{"escalate-wrapped", &EscalateError{Reason: "stuck"}, false, ""},
		{"deadline", context.DeadlineExceeded, true, "timeout"},
		{"truncated-sentinel", ErrTruncatedStream, true, "stream-drop"},
		{"truncated-wrapped", &TruncatedStreamError{Streak: 3}, true, "stream-drop"},
		{"two-strike", &TwoStrikeError{}, true, "wedged"},
		{"per-tool", &PerToolFailureError{Tool: "bash", Streak: 8}, true, "wedged"},
		{"format-strike", &FormatStrikeError{Streak: 3}, true, "wedged"},
		{"repeat-stream", &RepeatStreamError{Streak: 3}, true, "wedged"},
		{"empty-completion", &EmptyCompletionError{Streak: 2}, true, "wedged"},
		{"max-iter", &MaxIterationsError{Limit: 100}, true, "max-iter"},
		{"unknown-fatal", errors.New("provider down: 503"), false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ClassifyResume(c.err, RunResult{})
			if d.Resume != c.wantResume {
				t.Fatalf("Resume = %v, want %v", d.Resume, c.wantResume)
			}
			if d.Reason != c.wantReason {
				t.Fatalf("Reason = %q, want %q", d.Reason, c.wantReason)
			}
			if d.Resume && d.Continuation == "" {
				t.Fatalf("resumable decision has empty continuation")
			}
			if !d.Resume && d.Continuation != "" {
				t.Fatalf("non-resumable decision has continuation %q", d.Continuation)
			}
		})
	}
}

func TestIsWedge(t *testing.T) {
	wedges := []error{
		ErrTwoStrike, ErrPerToolFailureCap, ErrFormatStrike,
		ErrRepeatStream, ErrEmptyCompletion,
	}
	for _, e := range wedges {
		if !IsWedge(e) {
			t.Errorf("IsWedge(%v) = false, want true", e)
		}
	}
	notWedges := []error{
		nil, context.Canceled, context.DeadlineExceeded,
		ErrEscalate, ErrTruncatedStream, ErrMaxIterations,
		errors.New("random"),
	}
	for _, e := range notWedges {
		if IsWedge(e) {
			t.Errorf("IsWedge(%v) = true, want false", e)
		}
	}
}
