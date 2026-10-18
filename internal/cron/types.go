// Package cron provides a config-driven scheduler with daily active-hours
// gating and a per-resource busy queue, so overlapping schedules cannot stack.
// Scheduling decisions are driven by an injectable Clock and by Tick's
// explicit time argument, never by the wall clock directly.
package cron

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Clock supplies the scheduler's notion of "now". Tests inject a fake so no
// real time is needed to exercise scheduling.
type Clock interface{ Now() time.Time }

// realClock is the wall-clock Clock used when none is supplied.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Runner executes one job. It is called from its own goroutine and must be
// safe for concurrent use across jobs.
type Runner interface {
	Run(ctx context.Context, job *Job) error
}

// RunnerFunc adapts a plain function to Runner.
type RunnerFunc func(ctx context.Context, job *Job) error

// Run implements Runner.
func (f RunnerFunc) Run(ctx context.Context, job *Job) error { return f(ctx, job) }

// Job is one scheduled unit of work.
type Job struct {
	Name        string
	Command     string
	Schedule    *Schedule
	ResourceKey string
	Window      *Window

	nextDue  time.Time
	running  bool
	lastWall string // wall-clock minute of the last launch, see wallRepeat
}

// due reports whether the job's next scheduled time has arrived.
func (j *Job) due(now time.Time) bool { return !j.nextDue.After(now) }

// Schedule is a parsed five-field cron expression
// (minute hour day-of-month month day-of-week). Fields accept "*", lists,
// ranges, and "/step" suffixes; month and day names are not supported.
type Schedule struct {
	minute, hour, dom, month, dow cronField
}

type cronField struct {
	bits       uint64
	lo, hi     int
	restricted bool
}

func (f cronField) match(v int) bool {
	return v >= f.lo && v <= f.hi && f.bits&(1<<uint(v)) != 0
}

// ParseSchedule parses a five-field cron expression into a Schedule.
func ParseSchedule(expr string) (*Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: want 5 fields, got %d in %q", len(fields), expr)
	}
	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron minute: %w", err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron hour: %w", err)
	}
	dom, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-month: %w", err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron month: %w", err)
	}
	dow, err := parseCronField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-week: %w", err)
	}
	return &Schedule{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

func parseCronField(expr string, lo, hi int) (cronField, error) {
	f := cronField{lo: lo, hi: hi}
	full := uint64(0)
	for v := lo; v <= hi; v++ {
		full |= 1 << uint(v)
	}
	for _, part := range strings.Split(expr, ",") {
		if part == "" {
			return f, fmt.Errorf("empty list element")
		}
		step := 1
		stepped := false
		if i := strings.Index(part, "/"); i >= 0 {
			stepped = true
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return f, fmt.Errorf("bad step in %q", part)
			}
			step = n
			part = part[:i]
		}
		start, end, err := cronRange(part, lo, hi)
		if err != nil {
			return f, err
		}
		// "5/15" runs from 5 to the top of the field, not just at 5
		if stepped && start == end && part != "" && part != "*" && !strings.Contains(part, "-") {
			end = hi
		}
		for v := start; v <= end; v += step {
			f.bits |= 1 << uint(v)
		}
	}
	f.restricted = f.bits != full
	return f, nil
}

func cronRange(part string, lo, hi int) (int, int, error) {
	switch {
	case part == "*" || part == "":
		return lo, hi, nil
	case strings.Contains(part, "-"):
		ab := strings.SplitN(part, "-", 2)
		a, err1 := strconv.Atoi(ab[0])
		b, err2 := strconv.Atoi(ab[1])
		if err1 != nil || err2 != nil || a < lo || b > hi || a > b {
			return 0, 0, fmt.Errorf("bad range %q", part)
		}
		return a, b, nil
	default:
		n, err := strconv.Atoi(part)
		if err != nil || n < lo || n > hi {
			return 0, 0, fmt.Errorf("bad value %q", part)
		}
		return n, n, nil
	}
}

// wallRepeat reports whether the job already launched at this wall-clock
// minute and records it otherwise. A DST fall-back replays an hour, so a job
// pinned to specific hours would fire twice for one calendar time. Jobs with
// a free hour field are exempt: they are meant to recur each real hour.
func (j *Job) wallRepeat(now time.Time) bool {
	if !j.Schedule.hour.restricted {
		return false
	}
	wall := now.Format("2006-01-02 15:04")
	if wall == j.lastWall {
		return true
	}
	j.lastWall = wall
	return false
}

// Match reports whether t satisfies the schedule to minute resolution. When
// both day-of-month and day-of-week are restricted, either may match.
func (s *Schedule) Match(t time.Time) bool {
	if !s.minute.match(t.Minute()) || !s.hour.match(t.Hour()) || !s.month.match(int(t.Month())) {
		return false
	}
	domOK := s.dom.match(t.Day())
	dowOK := s.dow.match(int(t.Weekday()))
	if s.dom.restricted && s.dow.restricted {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// Next returns the first minute after after that matches the schedule, or the
// zero time if none matches within five years.
func (s *Schedule) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if s.Match(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}
