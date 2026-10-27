package zzz

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// a cancel is the operator stopping: it ends the run as aborted even when the
// streak counters sit one step from their limit.
func TestSettle_CancelledIterationIsAbortedNotFailed(t *testing.T) {
	for _, status := range []string{IterFailed, IterReset, IterNoop} {
		t.Run(status, func(t *testing.T) {
			d := &driver{cfg: Config{MaxConsecutiveFails: 3, MaxConsecutiveNoops: 3}, run: &Run{}, fails: 2, noops: 2}
			res := IterationResult{Status: status, Subject: "same words"}
			got, cause := d.settle(res, fmt.Errorf("engine: %w", context.Canceled))
			if got != StatusAborted || cause != "context canceled mid-iter" {
				t.Fatalf("settle = %q, %q; want aborted", got, cause)
			}
			if d.fails != 2 || d.noops != 2 {
				t.Errorf("cancel fed the streaks: fails=%d noops=%d", d.fails, d.noops)
			}
		})
	}
}

func TestSettle_RealFailureStillCountsAtLimit(t *testing.T) {
	d := &driver{cfg: Config{MaxConsecutiveFails: 3}, run: &Run{}, fails: 2}
	got, _ := d.settle(IterationResult{Status: IterFailed}, errors.New("boom"))
	if got != StatusFailed {
		t.Fatalf("settle = %q, want failed", got)
	}
}
