package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageRecord is one completed-turn usage line in the append-only usage log.
// One JSON object per line. Optionals carry omitempty so lines stay small.
type UsageRecord struct {
	Time         time.Time `json:"t"` // UTC, turn completion
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Input        int       `json:"in"`
	Output       int       `json:"out"`
	Cached       int       `json:"cached,omitempty"`   // cached input tokens, if reported
	USD          float64   `json:"usd,omitempty"`      // cost for this turn
	CostReported bool      `json:"reported,omitempty"` // true => provider $, false => static estimate
}

var usageMu sync.Mutex

// usagePath resolves the append-only usage log. BEE_USAGE_LOG pins it directly
// (used by tests); otherwise it lands inside BEE_HOME or ~/.bee. Empty when no
// home can be determined so callers degrade to a no-op.
func usagePath() string {
	if p := os.Getenv("BEE_USAGE_LOG"); p != "" {
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
	return filepath.Join(home, "usage.jsonl")
}

// AppendUsage appends one usage line. Unlike the lifetime counter this log is
// append-only — a single O_APPEND write of a short line is atomic on local
// filesystems, so a crash mid-write can at worst leave one trailing partial
// line, which the reader skips. I/O errors are swallowed: a missing or
// unwritable home must never crash a turn.
func AppendUsage(r UsageRecord) {
	if r.Input <= 0 && r.Output <= 0 {
		return
	}
	path := usagePath()
	if path == "" {
		return
	}
	r.Time = r.Time.UTC()
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// ResetUsageForTest removes the usage log so a test starts from empty. Test-
// only; resolves the same path AppendUsage writes to.
func ResetUsageForTest() {
	usageMu.Lock()
	defer usageMu.Unlock()
	if path := usagePath(); path != "" {
		_ = os.Remove(path)
	}
}
