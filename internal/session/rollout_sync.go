package session

import "time"

// syncDebounce is the minimum gap between background fsyncs. Appends that land
// while a sync runs or during the gap coalesce into the next one (group
// commit), so a tool-heavy loop costs at most ~10 syncs/s however fast it writes.
// Power-loss exposure is bounded by this gap plus one fsync.
const syncDebounce = 100 * time.Millisecond

// syncer is the background goroutine that fsyncs a Rollout off the Append path.
type syncer struct {
	wake chan struct{} // cap 1: pending-write signal, coalesced
	stop chan struct{}
	done chan struct{}
}

func (s *syncer) shutdown() {
	close(s.stop)
	<-s.done
}

// kickLocked starts the syncer on first use and signals it. Caller holds r.mu.
func (r *Rollout) kickLocked() {
	if r.closing {
		return
	}
	if r.sy == nil {
		r.sy = &syncer{
			wake: make(chan struct{}, 1),
			stop: make(chan struct{}),
			done: make(chan struct{}),
		}
		go r.syncLoop(r.sy)
	}
	select {
	case r.sy.wake <- struct{}{}:
	default:
	}
}

func (r *Rollout) syncLoop(s *syncer) {
	defer close(s.done)
	gap := time.NewTimer(0)
	defer gap.Stop()
	<-gap.C
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		}
		r.syncPending()
		gap.Reset(syncDebounce)
		select {
		case <-s.stop:
			return
		case <-gap.C:
		}
	}
}

// syncPending fsyncs when something was written since the last sync. The mutex
// is released before the fsync so Append never waits on the disk.
func (r *Rollout) syncPending() {
	r.mu.Lock()
	f, dirty := r.f, r.dirty
	r.dirty = false
	r.mu.Unlock()
	if f == nil || !dirty {
		return
	}
	if err := f.Sync(); err != nil {
		r.mu.Lock()
		r.dirty = true // retry on the next kick or at Close
		r.mu.Unlock()
	}
}

// Flush asks the syncer to fsync soon. The loop calls it at each turn boundary;
// it returns at once, since the data is already in the page cache and Close
// does the blocking final sync.
func (r *Rollout) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil && r.dirty {
		r.kickLocked()
	}
	return nil
}
