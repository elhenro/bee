package hive

import (
	"context"

	"github.com/elhenro/bee/internal/sysload"
)

// loadWait is swapped in tests so they never depend on real machine load.
var loadWait = sysload.Wait

// Throttle holds new workers while the machine is oversubscribed. Zero value
// never waits.
type Throttle struct {
	// MaxLoadRatio is the 1-minute load per core above which a worker waits
	// for its start; 0 disables.
	MaxLoadRatio float64
	// OnHold, when set, fires once per held worker with the load that held it.
	OnHold func(perCore float64)
}

// wait blocks until load allows a start or ctx ends.
func (t Throttle) wait(ctx context.Context) error {
	if t.MaxLoadRatio <= 0 {
		return nil
	}
	return loadWait(ctx, t.MaxLoadRatio, t.OnHold)
}
