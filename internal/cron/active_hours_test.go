package cron

import (
	"testing"
	"time"
)

func TestParseWindowAndIn(t *testing.T) {
	w, err := ParseWindow("09:00-17:00")
	if err != nil {
		t.Fatalf("ParseWindow: %v", err)
	}
	if !w.In(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("09:00 should be inside")
	}
	if w.In(time.Date(2026, 3, 1, 8, 59, 0, 0, time.UTC)) {
		t.Fatal("08:59 should be outside")
	}
	if w.In(time.Date(2026, 3, 1, 17, 0, 0, 0, time.UTC)) {
		t.Fatal("17:00 is the exclusive end and should be outside")
	}
}

func TestWindowWrapsMidnight(t *testing.T) {
	w, err := ParseWindow("22:00-06:00")
	if err != nil {
		t.Fatalf("ParseWindow: %v", err)
	}
	if !w.In(time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)) {
		t.Fatal("23:30 should be inside a wrapping window")
	}
	if !w.In(time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)) {
		t.Fatal("03:00 should be inside a wrapping window")
	}
	if w.In(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("12:00 should be outside a wrapping window")
	}
}

func TestWindowNextOpen(t *testing.T) {
	w, _ := ParseWindow("09:00-17:00")
	// Before opening on the same day.
	got := w.NextOpen(time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC))
	want := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextOpen = %s, want %s", got, want)
	}
	// After closing rolls to the next day.
	got = w.NextOpen(time.Date(2026, 3, 1, 18, 0, 0, 0, time.UTC))
	want = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextOpen = %s, want %s", got, want)
	}
	// Inside the window returns the input time.
	in := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if !w.NextOpen(in).Equal(in) {
		t.Fatalf("NextOpen inside window = %s, want %s", w.NextOpen(in), in)
	}
}

func TestParseWindowRejectsBadInput(t *testing.T) {
	for _, s := range []string{"", "09:00", "09:00-", "9:00-17:00-18:00", "25:00-17:00", "09:00-09:00"} {
		if _, err := ParseWindow(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}
