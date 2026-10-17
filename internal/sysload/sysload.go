// Package sysload reads the 1-minute load average so spawners can hold new
// workers while the machine is oversubscribed. Pure Go: sysctl on darwin,
// /proc/loadavg on linux, unavailable elsewhere.
package sysload

import (
	"context"
	"runtime"
	"time"
)

// pollEvery is how often Wait re-reads the load while holding a spawn.
var pollEvery = 5 * time.Second

// read is swapped in tests.
var read = loadAvg1

// PerCore returns the 1-minute load average divided by the CPU count. ok is
// false when the platform exposes no load average.
func PerCore() (float64, bool) {
	l, ok := read()
	if !ok {
		return 0, false
	}
	return l / float64(runtime.NumCPU()), true
}

// Wait blocks while load per core is above ratio. ratio <= 0 or an unknown
// load returns at once, and so does a poll where release (optional) reports
// true: callers use it so a hold slows work down without starving it. onHold
// fires once, on the first poll that holds, so callers can say why nothing is
// starting. Returns ctx.Err() on cancel.
func Wait(ctx context.Context, ratio float64, onHold func(perCore float64), release func() bool) error {
	if ratio <= 0 {
		return nil
	}
	held := false
	for {
		l, ok := PerCore()
		if !ok || l <= ratio || (release != nil && release()) {
			return nil
		}
		if !held && onHold != nil {
			onHold(l)
		}
		held = true
		t := time.NewTimer(pollEvery)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}
