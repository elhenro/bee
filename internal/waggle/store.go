package waggle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/beehome"
)

// Store is a directory of waggle skill files for one scope. Project stores are
// keyed by a hash of the project root (the same scheme as the checkpoint store)
// so path-bearing routes never leak between projects; the user store is shared.
type Store struct {
	dir string
}

// ProjectStore returns the per-project waggle store, rooted at
// <beeHome>/waggle/proj/<hash>/skills.
func ProjectStore(projectRoot string) (*Store, error) {
	sum := sha256.Sum256([]byte(projectRoot))
	return storeAt("proj", hex.EncodeToString(sum[:])[:16])
}

// UserStore returns the cross-project waggle store at
// <beeHome>/waggle/user/skills.
func UserStore() (*Store, error) { return storeAt("user") }

func storeAt(sub ...string) (*Store, error) {
	dir, err := beehome.Join(slices.Concat([]string{"waggle"}, sub, []string{"skills"})...)
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir is the absolute skills directory for this store.
func (s *Store) Dir() string { return s.dir }

// LedgerPath is the reuse-log path for this store, a sibling of the skills dir.
func (s *Store) LedgerPath() string { return filepath.Join(filepath.Dir(s.dir), "ledger.jsonl") }

// Exists reports whether a waggle named name is already stored.
func (s *Store) Exists(name string) bool {
	_, err := os.Stat(filepath.Join(s.dir, name+".md"))
	return err == nil
}

// IsDisabled reports whether the named waggle was demoted by curation. The
// on-demand lookup path uses this to refuse a retired route.
func (s *Store) IsDisabled(name string) bool {
	return isDisabled(filepath.Join(s.dir, name+".md"))
}

// Write persists the waggle markdown as <name>.md, creating the dir on demand.
func (s *Store) Write(name, md string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	// 0600: mined commands may carry paths and arguments worth keeping private
	return atomicfile.Write(filepath.Join(s.dir, name+".md"), []byte(md), 0o600)
}

// mdNames lists the stored waggle file names in name order. A missing dir is an
// empty store, not an error.
func (s *Store) mdNames() ([]string, error) {
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	return names, err
}

// sweep calls fn with each waggle's name and path and counts the files fn
// reports as changed.
func (s *Store) sweep(fn func(name, path string) bool) (int, error) {
	names, err := s.mdNames()
	changed := 0
	for _, n := range names {
		if fn(strings.TrimSuffix(n, ".md"), filepath.Join(s.dir, n)) {
			changed++
		}
	}
	return changed, err
}
