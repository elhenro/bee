package gate

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// heavyPoll is how often a waiting gate retries the heavy lock.
var heavyPoll = 250 * time.Millisecond

// HeavyLockPath is <BEE_HOME or ~/.bee>/locks/heavy, shared by every bee on
// the machine.
func HeavyLockPath() (string, error) {
	root := os.Getenv("BEE_HOME")
	if root == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(h, ".bee")
	}
	d := filepath.Join(root, "locks")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(d, "heavy"), nil
}

// lockHeavy blocks until this process holds the heavy lock or ctx ends. The
// kernel drops a crashed holder's flock, so there is no stale-pid reaping.
func lockHeavy(ctx context.Context) (func(), error) {
	p, err := HeavyLockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := tryFlock(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if ok {
			return func() { unflock(f); _ = f.Close() }, nil
		}
		t := time.NewTimer(heavyPoll)
		select {
		case <-ctx.Done():
			t.Stop()
			_ = f.Close()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}
