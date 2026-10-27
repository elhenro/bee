package cost

import "sync"

// the usage log and the lifetime counter are persisted by one on-demand
// background goroutine, so a turn never waits on mkdir, flock or rename.
// Flush is the barrier for exit paths and for readers of the files.

type usageJob struct {
	path string
	rec  UsageRecord
}

var (
	bgMu      sync.Mutex
	bgIdle    = sync.NewCond(&bgMu)
	bgJobs    []usageJob
	bgRunning bool
)

func enqueueUsage(path string, r UsageRecord) {
	bgMu.Lock()
	bgJobs = append(bgJobs, usageJob{path, r})
	startWriterLocked()
	bgMu.Unlock()
}

func kickWriter() {
	bgMu.Lock()
	startWriterLocked()
	bgMu.Unlock()
}

func startWriterLocked() {
	if !bgRunning {
		bgRunning = true
		go drainWriter()
	}
}

// drainWriter writes queued work until none is left, then exits.
func drainWriter() {
	for {
		bgMu.Lock()
		jobs := bgJobs
		bgJobs = nil
		if len(jobs) == 0 && !lifetimeDirty() {
			bgRunning = false
			bgIdle.Broadcast()
			bgMu.Unlock()
			return
		}
		bgMu.Unlock()
		for _, j := range jobs {
			writeUsage(j.path, j.rec)
		}
		flushLifetime()
	}
}

// Flush blocks until every usage line and lifetime delta queued so far is on
// disk. Call it before the process exits and before reading either file.
func Flush() {
	bgMu.Lock()
	for bgRunning {
		bgIdle.Wait()
	}
	bgMu.Unlock()
}
