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
}

// Ledger is an append-only reuse log for one scope, persisted as JSONL beside
// that scope's skills dir. Appends are serialized so concurrent replays in one
// session can't interleave a line.
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

// Stat is a waggle's aggregated reuse: how many times it was followed and the
// total estimated tokens saved.
type Stat struct {
	Uses  int
	Yield int
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
		s.Uses++
		s.Yield += e.Yield
		out[e.Name] = s
	}
	return out, sc.Err()
}
