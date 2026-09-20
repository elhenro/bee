package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Window is a daily active-hours window expressed as two local times-of-day.
// A window whose end precedes its start wraps past midnight (for example
// "22:00-06:00").
type Window struct {
	start time.Duration
	end   time.Duration
}

// ParseWindow parses "HH:MM-HH:MM" into a daily window.
func ParseWindow(s string) (*Window, error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("cron: active_hours want HH:MM-HH:MM, got %q", s)
	}
	start, err := parseClock(parts[0])
	if err != nil {
		return nil, err
	}
	end, err := parseClock(parts[1])
	if err != nil {
		return nil, err
	}
	if start == end {
		return nil, fmt.Errorf("cron: active_hours window %q is empty", s)
	}
	return &Window{start: start, end: end}, nil
}

func parseClock(s string) (time.Duration, error) {
	hm := strings.Split(strings.TrimSpace(s), ":")
	if len(hm) != 2 {
		return 0, fmt.Errorf("cron: bad time %q", s)
	}
	h, err1 := strconv.Atoi(hm[0])
	m, err2 := strconv.Atoi(hm[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("cron: bad time %q", s)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

// In reports whether t falls inside the window. A nil window is always open.
func (w *Window) In(t time.Time) bool {
	if w == nil {
		return true
	}
	d := time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
	if w.start < w.end {
		return d >= w.start && d < w.end
	}
	return d >= w.start || d < w.end
}

// NextOpen returns the next time at or after t at which the window is open.
// A nil window returns t unchanged.
func (w *Window) NextOpen(t time.Time) time.Time {
	if w == nil || w.In(t) {
		return t
	}
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	open := midnight.Add(w.start)
	if !open.After(t) {
		open = open.Add(24 * time.Hour)
	}
	return open
}
