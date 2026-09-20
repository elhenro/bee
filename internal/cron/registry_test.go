package cron

import (
	"testing"
	"time"
)

func TestParseScheduleAndNext(t *testing.T) {
	s, err := ParseSchedule("30 12 * * *")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	got := s.Next(base)
	want := time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Next = %s, want %s", got, want)
	}
	if !s.Match(time.Date(2026, 3, 5, 12, 30, 0, 0, time.UTC)) {
		t.Fatal("expected 12:30 to match")
	}
	if s.Match(time.Date(2026, 3, 5, 12, 31, 0, 0, time.UTC)) {
		t.Fatal("expected 12:31 not to match")
	}
}

func TestParseScheduleStepsAndLists(t *testing.T) {
	s, err := ParseSchedule("0,15,30,45 */6 * * 1-5")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	if !s.Match(time.Date(2026, 3, 2, 6, 15, 0, 0, time.UTC)) { // Monday 06:15
		t.Fatal("expected Monday 06:15 to match")
	}
	if s.Match(time.Date(2026, 3, 1, 6, 15, 0, 0, time.UTC)) { // Sunday
		t.Fatal("expected Sunday to be excluded by day-of-week")
	}
	if s.Match(time.Date(2026, 3, 2, 7, 15, 0, 0, time.UTC)) {
		t.Fatal("expected hour 7 to be excluded by step")
	}
}

func TestParseScheduleRejectsBadInput(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "x * * * *"} {
		if _, err := ParseSchedule(expr); err == nil {
			t.Fatalf("expected error for %q", expr)
		}
	}
}

func TestRegistryAddDefaultsAndValidation(t *testing.T) {
	r := NewRegistry()
	j, err := r.Add(JobSpec{Name: "nightly", Schedule: "0 2 * * *", Command: "bee plan"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if j.ResourceKey != "nightly" {
		t.Fatalf("ResourceKey = %q, want default to name", j.ResourceKey)
	}
	if j.Window != nil {
		t.Fatal("expected nil window when active_hours omitted")
	}
	if r.Len() != 1 || len(r.Jobs()) != 1 {
		t.Fatalf("registry len = %d", r.Len())
	}
	if _, err := r.Add(JobSpec{Name: "nightly", Schedule: "0 2 * * *"}); err == nil {
		t.Fatal("expected duplicate-name error")
	}
	if _, err := r.Add(JobSpec{Name: "x"}); err == nil {
		t.Fatal("expected missing-schedule error")
	}
	if _, err := r.Add(JobSpec{Name: "y", Schedule: "0 2 * * *", ActiveHours: "bad"}); err == nil {
		t.Fatal("expected bad active_hours error")
	}
	if _, err := r.Add(JobSpec{Name: "z", Schedule: "nope"}); err == nil {
		t.Fatal("expected bad schedule error")
	}
}
