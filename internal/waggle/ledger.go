package waggle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/flock"
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
// the shared path) the file lock orders appends against CompactLedger's rewrite.
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
	defer flock.Lock(l.path)()
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

// scanLedger calls fn for every well-formed named entry in a JSONL ledger,
// with the entry's raw line. Malformed lines are skipped.
func scanLedger(r io.Reader, fn func(e LedgerEntry, line []byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		var e LedgerEntry
		if len(line) == 0 || json.Unmarshal(line, &e) != nil || e.Name == "" {
			continue
		}
		fn(e, line)
	}
	return sc.Err()
}

// CompactLedger rewrites a ledger, keeping only entries whose Name is in keep.
// Run it after deleting waggle files so a later re-mine of the same (content-
// stable) name starts with clean stats instead of inheriting a dead route's
// history — otherwise stale Fails would auto-demote the fresh route. A missing
// ledger is a no-op.
func CompactLedger(path string, keep map[string]bool) error {
	// held across read and rename so a concurrent Append can't land in between
	// and be dropped by the rewrite
	defer flock.Lock(path)()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	err = scanLedger(f, func(e LedgerEntry, line []byte) {
		if keep[e.Name] {
			buf.Write(line)
			buf.WriteByte('\n')
		}
	})
	f.Close()
	if err != nil {
		return err
	}
	// atomic temp+rename: a concurrent Append (O_APPEND) or reader never sees a
	// torn/truncated ledger mid-rewrite.
	return atomicfile.Write(path, buf.Bytes(), 0o644)
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
	err = scanLedger(f, func(e LedgerEntry, _ []byte) {
		s := out[e.Name]
		if e.Fail {
			s.Fails++
		} else {
			s.Uses++
			s.Yield += e.Yield
		}
		out[e.Name] = s
	})
	return out, err
}

// RecordUse logs one explicit follow of a waggle (the on-demand lookup path or
// `bee <name>`), so curation sees uses beyond predictive replay and does not
// prune a route that is only ever run by hand. Best-effort.
func RecordUse(s *Store, name string, yield int) error {
	if s == nil {
		return nil
	}
	return NewLedger(s.LedgerPath()).Append(LedgerEntry{Name: name, Steps: 1, Yield: yield})
}

// RecordUseByName is RecordUse for a caller that only knows the waggle name and
// cwd (the `bee <name>` dispatcher): it logs to the first of the project and
// user stores that holds the route.
func RecordUseByName(cwd, name string, yield int) {
	proj, _ := ProjectStore(cwd)
	user, _ := UserStore()
	for _, s := range []*Store{proj, user} {
		if s != nil && s.Exists(name) {
			_ = RecordUse(s, name, yield)
			return
		}
	}
}
