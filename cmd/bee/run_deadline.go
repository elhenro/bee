package main

import (
	"context"

	"github.com/elhenro/bee/internal/config"
)

// turnContext bounds one headless attempt. With the watchdog off and no
// explicit timeout_seconds there is no deadline: the default 10m cap belongs
// to the auto-resume safety net, and a disabled net must not kill a long
// active turn.
func turnContext(parent context.Context, w config.WatchdogConfig) (context.Context, context.CancelFunc) {
	if !w.Enabled && w.TimeoutSec <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, w.Timeout())
}
