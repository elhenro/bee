package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// emptyMaxAge spares a rollout another process just created and has not
// written to yet; only abandoned zero-byte files older than this go.
const emptyMaxAge = time.Hour

var pruneEmptyOnce sync.Once

// pruneEmpty removes zero-byte rollouts left by starts that never took a turn,
// once per process. keep is the caller's own file. best-effort.
func pruneEmpty(dir, keep string) {
	pruneEmptyOnce.Do(func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		cutoff := time.Now().Add(-emptyMaxAge)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if p == keep {
				continue
			}
			if fi, err := e.Info(); err == nil && fi.Size() == 0 && fi.ModTime().Before(cutoff) {
				_ = os.Remove(p)
			}
		}
	})
}
