package waggle

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/skills"
)

// Meta is a stored waggle's display info for `bee waggle ls`. Kind is "exec"
// for a replayable read-only route and "recipe" for a mutating one that only
// runs as `bee <name>`.
type Meta struct {
	Name        string
	Kind        string
	Description string
	Script      string
	Path        string
}

// List returns the waggles in a store, sorted by name. A missing store dir is
// not an error (returns nil); unparseable files are skipped.
func List(s *Store) ([]Meta, error) {
	names, err := s.mdNames()
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, n := range names {
		p := filepath.Join(s.dir, n)
		if isDisabled(p) {
			continue // demoted route: hidden from ls, the leaderboard, and lookup
		}
		sk, err := skills.ParseFile(p)
		if err != nil {
			continue
		}
		out = append(out, Meta{Name: sk.Name, Kind: string(sk.Kind), Description: sk.Description, Script: scriptOfSkill(sk), Path: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GC prunes a store: it removes files that fail to parse, have no script, or
// duplicate a script already kept. Returns the number removed. Safe to run any
// time — it only touches files under the waggle store, which bee owns.
func GC(s *Store) (int, error) {
	seen := map[string]bool{} // names come in sorted order: first occurrence of a script wins
	return s.sweep(func(_, p string) bool {
		script := ""
		if sk, err := skills.ParseFile(p); err == nil {
			script = scriptOfSkill(sk)
		}
		if script == "" || seen[script] {
			return os.Remove(p) == nil
		}
		seen[script] = true
		return false
	})
}

// scriptOfSkill is the dedup/display key of a stored waggle: the exec script
// for an exec waggle, the shell steps joined for a recipe waggle.
func scriptOfSkill(sk skills.Skill) string {
	if sk.Kind == skills.KindRecipe && sk.Runtime {
		cmds := make([]string, 0, len(sk.Steps))
		for _, st := range sk.Steps {
			if st.Kind == skills.StepShell {
				cmds = append(cmds, st.Run)
			}
		}
		return strings.Join(cmds, "; ")
	}
	return scriptFromExec(sk.Exec)
}

// scriptFromExec extracts the bash script from a [bash, -c, <script>] vector.
func scriptFromExec(exec []string) string {
	if len(exec) == 3 && exec[0] == "bash" && exec[1] == "-c" {
		return exec[2]
	}
	return ""
}
