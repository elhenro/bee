package waggle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// LedgerEntry is one recorded reuse of a waggle. Yield is the estimated tokens
// saved by collapsing the route's remaining round-trips (chars/4), not a
// measured counterfactual — useful for ranking and curation, not accounting.
type LedgerEntry struct {
	Name  string `json:"name"`
	Steps int    `json:"steps"`
	Yield int    `json:"yield"`
	// Fail marks a divergence: the route matched a prefix but its tail exec
	// failed or returned nothing. Recorded with zero yield; curation demotes a
	// waggle that keeps diverging without ever paying off.
	Fail bool `json:"fail,omitempty"`
}

// Ledger is an append-only reuse log for one scope, persisted as JSONL beside
// that scope's skills dir. The mutex serializes appends within a single Ledger
// instance; across instances/processes (each hive worker builds its own over
// the shared path) line integrity relies on O_APPEND atomicity for the short
// single-line writes, not on the mutex.
type Ledger struct {
	mu   sync.Mutex
	path string
}

// NewLedger returns a ledger writing to path. A nil ledger is a no-op sink.
func NewLedger(path string) *Ledger { return &Ledger{path: path} }

// Append records one reuse event, creating the file and parent dir on demand.
func (l *Ledger) Append(e LedgerEntry) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// CompactLedger rewrites a ledger, keeping only entries whose Name is in keep.
// Run it after deleting waggle files so a later re-mine of the same (content-
// stable) name starts with clean stats instead of inheriting a dead route's
// history — otherwise stale Fails would auto-demote the fresh route. A missing
// ledger is a no-op.
func CompactLedger(path string, keep map[string]bool) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var kept [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e LedgerEntry
		if json.Unmarshal(line, &e) != nil || e.Name == "" || !keep[e.Name] {
			continue
		}
		kept = append(kept, append([]byte(nil), line...))
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, l := range kept {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	// atomic temp+rename: a concurrent Append (O_APPEND) or reader never sees a
	// torn/truncated ledger mid-rewrite.
	return writeFileAtomic(path, buf.Bytes(), 0o644)
}

// Stat is a waggle's aggregated reuse: how many times it was followed and the
// total estimated tokens saved.
type Stat struct {
	Uses  int
	Yield int
	Fails int // recorded divergences (replay matched but its tail exec failed)
}

// ReadLedger aggregates a JSONL ledger by waggle name. A missing file is not an
// error (returns an empty map); malformed lines are skipped.
func ReadLedger(path string) (map[string]Stat, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]Stat{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]Stat{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e LedgerEntry
		if json.Unmarshal(line, &e) != nil || e.Name == "" {
			continue
		}
		s := out[e.Name]
		if e.Fail {
			s.Fails++
		} else {
			s.Uses++
			s.Yield += e.Yield
		}
		out[e.Name] = s
	}
	return out, sc.Err()
}
