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
	lifeData   lifetimeFile
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
	lifeMu.Lock()
	lifeLoaded = false
	lifeData = lifetimeFile{}
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
// bee session that ever called AddLifetime.
func LifetimeTotals() (in, out int64) {
	lifeMu.Lock()
	defer lifeMu.Unlock()
	ensureLifetimeLoaded()
	return lifeData.Input, lifeData.Output
}

// AddLifetime bumps persisted totals and writes the file atomically. I/O
// errors are swallowed: a missing or unwritable home should never crash a
// turn, and on next start the counter just stays at the last successful sum.
func AddLifetime(in, out int) {
	if in <= 0 && out <= 0 {
		return
	}
	lifeMu.Lock()
	defer lifeMu.Unlock()
	ensureLifetimeLoaded()
	if lifePath == "" {
		// no persistence target; keep the in-memory tally for the banner.
		lifeData.add(in, out)
		return
	}
	if err := os.MkdirAll(filepath.Dir(lifePath), 0o755); err != nil {
		return
	}
	// merge-on-write under a cross-process flock: concurrent bee processes
	// (fan, swarm, parallel headless runs) each re-read the file before
	// adding their delta, so nobody's counts get silently dropped by a
	// last-writer-wins overwrite.
	defer flock.Lock(lifePath)()
	lifeData = readLifetime(lifePath)
	lifeData.add(in, out)
	b, err := json.Marshal(lifeData)
	if err != nil {
		return
	}
	_ = atomicfile.Write(lifePath, b, 0o644)
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
