package waggle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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
	home, err := beeHome()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(projectRoot))
	hash := hex.EncodeToString(sum[:])[:16]
	return &Store{dir: filepath.Join(home, "waggle", "proj", hash, "skills")}, nil
}

// UserStore returns the cross-project waggle store at
// <beeHome>/waggle/user/skills.
func UserStore() (*Store, error) {
	home, err := beeHome()
	if err != nil {
		return nil, err
	}
	return &Store{dir: filepath.Join(home, "waggle", "user", "skills")}, nil
}

// Dir is the absolute skills directory for this store.
func (s *Store) Dir() string { return s.dir }

// Exists reports whether a waggle named name is already stored.
func (s *Store) Exists(name string) bool {
	_, err := os.Stat(filepath.Join(s.dir, name+".md"))
	return err == nil
}

// Write persists the waggle markdown as <name>.md, creating the dir on demand.
func (s *Store) Write(name, md string) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, name+".md"), []byte(md), 0o644)
}

// beeHome resolves ~/.bee, overridable via BEE_HOME for hermetic tests. Mirrors
// the resolution used by the checkpoint store and skills base dir.
func beeHome() (string, error) {
	if root := os.Getenv("BEE_HOME"); root != "" {
		return root, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".bee"), nil
}
