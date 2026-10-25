package hive

import (
	"context"
	"sync/atomic"

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

// start blocks until a worker may start, then counts it in running; call the
// returned func when it ends. Only extra workers wait: with no sibling
// running the start is immediate, and a held worker goes as soon as its
// siblings finish, so a busy machine slows the hive down but never stalls it.
func (t Throttle) start(ctx context.Context, running *atomic.Int32) (func(), error) {
	end := func() { running.Add(-1) }
	if t.MaxLoadRatio <= 0 {
		running.Add(1)
		return end, nil
	}
	// compare-and-swap so two workers racing past an idle hive count as one
	// first worker and one extra, never two firsts
	if running.CompareAndSwap(0, 1) {
		return end, nil
	}
	alone := func() bool { return running.Load() == 0 }
	if err := loadWait(ctx, t.MaxLoadRatio, t.OnHold, alone); err != nil {
		return nil, err
	}
	running.Add(1)
	return end, nil
}

// acquire takes a slot on sem, then waits out the load throttle. release frees
// both. A ctx that ends first returns its error and holds nothing.
func (t Throttle) acquire(ctx context.Context, sem chan struct{}, running *atomic.Int32) (release func(), err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case sem <- struct{}{}:
	}
	// cancellation may have raced the slot: don't run late workers
	if err := ctx.Err(); err != nil {
		<-sem
		return nil, err
	}
	end, err := t.start(ctx, running)
	if err != nil {
		<-sem
		return nil, err
	}
	return func() { end(); <-sem }, nil
}
