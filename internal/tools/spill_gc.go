package tools

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// spillMaxAge is how long a spilled tool body stays readable. the pointer in a
// truncated result is only useful within the run that got it.
const spillMaxAge = 7 * 24 * time.Hour

var spillGCOnce sync.Once

// gcSpill removes spill files older than spillMaxAge, once per process.
// best-effort: unreadable entries are skipped.
func gcSpill(dir string) {
	spillGCOnce.Do(func() { pruneOlderThan(dir, spillMaxAge) })
}

func pruneOlderThan(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
