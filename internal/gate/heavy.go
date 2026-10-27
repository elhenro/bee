package gate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/elhenro/bee/internal/beehome"
	"github.com/elhenro/bee/internal/pidlock"
)

// heavyPoll is how often a waiting gate retries the heavy lock.
var heavyPoll = 250 * time.Millisecond

// lockHeavy blocks until this process holds the heavy lock or ctx ends. The
// lock is <BEE_HOME or ~/.bee>/locks/heavy, shared by every bee on the machine.
// The kernel drops a crashed holder's flock, so there is no stale-pid reaping.
func lockHeavy(ctx context.Context) (func(), error) {
	p, err := beehome.Join("locks", "heavy")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	for {
		l, err := pidlock.Acquire(p, "gate")
		if _, ok := errors.AsType[*pidlock.HeldError](err); !ok {
			if err != nil {
				return nil, err
			}
			return l.Release, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(heavyPoll):
		}
	}
}
