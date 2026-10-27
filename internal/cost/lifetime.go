package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/beehome"
	"github.com/elhenro/bee/internal/flock"
)

// lifetimeFile is the JSON shape persisted to disk. int64 so a busy user
// running bee daily for years still can't overflow.
type lifetimeFile struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

var (
	lifeMu     sync.Mutex
	lifeLoaded bool
	lifeData   lifetimeFile // persisted totals as of the last read or write
	lifePend   lifetimeFile // added since, not yet written
	lifePath   string
)

// statePath resolves a state file: env pins it directly (used by tests),
// otherwise it lands inside the bee home. Empty when no home directory can be
// determined so callers degrade to a no-op.
func statePath(env, name string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	p, _ := beehome.Join(name)
	return p
}

// ResetLifetimeForTest clears the cached load so the next call re-reads from
// disk. Test-only; production never hot-reloads.
func ResetLifetimeForTest() {
	Flush()
	lifeMu.Lock()
	lifeLoaded = false
	lifeData = lifetimeFile{}
	lifePend = lifetimeFile{}
	lifePath = ""
	lifeMu.Unlock()
}

// readLifetime loads the persisted totals; a missing or corrupt file is zero.
func readLifetime(path string) (f lifetimeFile) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f
}

func (f *lifetimeFile) add(in, out int) {
	f.Input += int64(max(in, 0))
	f.Output += int64(max(out, 0))
}

func (f lifetimeFile) zero() bool { return f.Input == 0 && f.Output == 0 }

// ensureLifetimeLoaded is a lazy one-shot load. Caller holds lifeMu.
func ensureLifetimeLoaded() {
	if lifeLoaded {
		return
	}
	lifeLoaded = true
	lifePath = statePath("BEE_LIFETIME_TOKENS", "lifetime_tokens.json")
	if lifePath != "" {
		lifeData = readLifetime(lifePath)
	}
}

// LifetimeTotals returns persisted input/output token totals across every
// bee session that ever called AddLifetime. It waits for the background
// writer, so it counts deltas still queued; callers are banner and pane
// renders, never the turn path.
func LifetimeTotals() (in, out int64) {
	Flush()
	lifeMu.Lock()
	defer lifeMu.Unlock()
	ensureLifetimeLoaded()
	return lifeData.Input, lifeData.Output
}

// AddLifetime queues a bump of the persisted totals for the background writer
// (Flush waits for it). I/O errors are swallowed: a missing or unwritable home
// should never crash a turn, and on next start the counter just stays at the
// last successful sum.
func AddLifetime(in, out int) {
	if in <= 0 && out <= 0 {
		return
	}
	lifeMu.Lock()
	lifePend.add(in, out)
	lifeMu.Unlock()
	kickWriter()
}

// lifetimeDirty reports whether a delta still waits for the writer.
func lifetimeDirty() bool {
	lifeMu.Lock()
	defer lifeMu.Unlock()
	return !lifePend.zero()
}

// flushLifetime writes the queued delta, off the turn path. A failed write
// drops the delta from the file but keeps it in the in-memory tally.
func flushLifetime() {
	lifeMu.Lock()
	ensureLifetimeLoaded()
	p, path := lifePend, lifePath
	lifeMu.Unlock()
	if p.zero() {
		return
	}
	merged, ok := lifetimeFile{}, false
	if path != "" {
		merged, ok = mergeLifetime(path, p)
	}
	lifeMu.Lock()
	defer lifeMu.Unlock()
	lifePend.Input -= p.Input
	lifePend.Output -= p.Output
	if ok {
		lifeData = merged
		return
	}
	lifeData.Input += p.Input
	lifeData.Output += p.Output
}

// mergeLifetime adds p to the file under a cross-process flock: concurrent bee
// processes (fan, swarm, parallel headless runs) each re-read the file before
// adding their delta, so nobody's counts get silently dropped by a
// last-writer-wins overwrite.
func mergeLifetime(path string, p lifetimeFile) (lifetimeFile, bool) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return lifetimeFile{}, false
	}
	defer flock.Lock(path)()
	f := readLifetime(path)
	f.Input += p.Input
	f.Output += p.Output
	b, err := json.Marshal(f)
	if err != nil {
		return lifetimeFile{}, false
	}
	if atomicfile.Write(path, b, 0o600) != nil {
		return lifetimeFile{}, false
	}
	return f, true
}

// FormatLifetimeTokens compacts a token count for banner display: thousands
// as K, millions as M, billions as B. Two significant digits below 10, one
// digit above so the column doesn't grow as totals climb.
func FormatLifetimeTokens(total int64) string {
	switch {
	case total < 0:
		return "0"
	// units switch where the rounded figure would hit 1000, not at the exact power
	case total >= 999_500_000:
		return trimDecimal(float64(total)/1e9) + "B"
	case total >= 999_500:
		return trimDecimal(float64(total)/1e6) + "M"
	case total >= 1_000:
		return trimDecimal(float64(total)/1e3) + "K"
	default:
		return fmt.Sprintf("%d", total)
	}
}

func trimDecimal(v float64) string {
	if v >= 10 {
		return fmt.Sprintf("%.0f", v)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
}
