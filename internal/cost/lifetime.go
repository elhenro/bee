package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// lifetimePath resolves the totals file location. BEE_LIFETIME_TOKENS pins it
// directly (used by tests); otherwise lands inside BEE_HOME or ~/.bee. Empty
// when no home directory can be determined so callers degrade to a no-op.
func lifetimePath() string {
	if p := os.Getenv("BEE_LIFETIME_TOKENS"); p != "" {
		return p
	}
	home := os.Getenv("BEE_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".bee")
	}
	return filepath.Join(home, "lifetime_tokens.json")
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

// ensureLifetimeLoaded is a lazy one-shot load. Caller holds lifeMu.
func ensureLifetimeLoaded() {
	if lifeLoaded {
		return
	}
	lifeLoaded = true
	lifePath = lifetimePath()
	if lifePath == "" {
		return
	}
	b, err := os.ReadFile(lifePath)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &lifeData)
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
	if in > 0 {
		lifeData.Input += int64(in)
	}
	if out > 0 {
		lifeData.Output += int64(out)
	}
	if lifePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(lifePath), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(lifeData)
	if err != nil {
		return
	}
	tmp := lifePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, lifePath)
}

// FormatLifetimeTokens compacts a token count for banner display: thousands
// as K, millions as M, billions as B. Two significant digits below 10, one
// digit above so the column doesn't grow as totals climb.
func FormatLifetimeTokens(total int64) string {
	switch {
	case total < 0:
		return "0"
	case total >= 1_000_000_000:
		return trimDecimal(float64(total)/1e9) + "B"
	case total >= 1_000_000:
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
	s := fmt.Sprintf("%.1f", v)
	if len(s) >= 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}
