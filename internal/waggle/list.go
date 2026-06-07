package waggle

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/skills"
)

// Meta is a stored waggle's display info for `bee waggle ls`.
type Meta struct {
	Name        string
	Description string
	Script      string
	Path        string
}

// List returns the waggles in a store, sorted by name. A missing store dir is
// not an error (returns nil); unparseable files are skipped.
func List(s *Store) ([]Meta, error) {
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(s.dir, e.Name())
		sk, err := skills.ParseFile(p)
		if err != nil {
			continue
		}
		out = append(out, Meta{Name: sk.Name, Description: sk.Description, Script: scriptFromExec(sk.Exec), Path: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GC prunes a store: it removes files that fail to parse, have no script, or
// duplicate a script already kept. Returns the number removed. Safe to run any
// time — it only touches files under the waggle store, which bee owns.
func GC(s *Store) (int, error) {
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // deterministic: first occurrence of a script wins
	seen := map[string]bool{}
	removed := 0
	for _, n := range names {
		p := filepath.Join(s.dir, n)
		sk, err := skills.ParseFile(p)
		script := ""
		if err == nil {
			script = scriptFromExec(sk.Exec)
		}
		if script == "" || seen[script] {
			if os.Remove(p) == nil {
				removed++
			}
			continue
		}
		seen[script] = true
	}
	return removed, nil
}

// scriptFromExec extracts the bash script from a [bash, -c, <script>] vector.
func scriptFromExec(exec []string) string {
	if len(exec) == 3 && exec[0] == "bash" && exec[1] == "-c" {
		return exec[2]
	}
	return ""
}
