package waggle

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	yaml "github.com/goccy/go-yaml"

	"github.com/elhenro/bee/internal/skills"
)

// PruneStale removes waggles a store never paid off: zero recorded uses (per the
// ledger stats) and a file older than maxAge. Returns the number removed. now is
// injected so tests are deterministic. Waggles with any use are always kept.
func PruneStale(s *Store, stats map[string]Stat, maxAge time.Duration, now time.Time) (int, error) {
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		if stats[name].Uses > 0 {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < maxAge {
			continue
		}
		if os.Remove(filepath.Join(s.dir, e.Name())) == nil {
			removed++
		}
	}
	return removed, nil
}

// Promote copies any route present in >= 2 distinct project stores into the user
// store, so a route the agent rediscovers across projects becomes portable. The
// copy is re-tagged scope: user. Routes already in the user store are skipped.
// Returns the number promoted.
func Promote(user *Store) (int, error) {
	dirs, err := projectStoreDirs()
	if err != nil {
		return 0, err
	}
	holders := map[string]map[string]bool{} // script -> set of project dirs
	sample := map[string]string{}           // script -> a source .md path
	for _, d := range dirs {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(d, e.Name())
			sk, err := skills.ParseFile(p)
			if err != nil {
				continue
			}
			script := scriptFromExec(sk.Exec)
			if script == "" {
				continue
			}
			if holders[script] == nil {
				holders[script] = map[string]bool{}
			}
			holders[script][d] = true
			sample[script] = p
		}
	}
	promoted := 0
	for script, set := range holders {
		if len(set) < 2 {
			continue
		}
		name := waggleName(script)
		if user.Exists(name) {
			continue
		}
		if promoteFile(sample[script], user, name) == nil {
			promoted++
		}
	}
	return promoted, nil
}

// promoteFile re-tags a source waggle as user scope and writes it to the user
// store under name, preserving its frontmatter (name/type/exec/route/params).
func promoteFile(srcPath string, user *Store, name string) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	fmBytes, ok := frontmatterBytes(raw)
	if !ok {
		return os.ErrInvalid
	}
	var fm frontmatter
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return err
	}
	fm.Scope = string(ScopeUser)
	y, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	return user.Write(name, "---\n"+string(y)+"---\nCrystallized read-only route. Auto-generated waggle (promoted).\n")
}

// projectStoreDirs returns every per-project skills dir under the waggle home.
func projectStoreDirs() ([]string, error) {
	home, err := beeHome()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(home, "waggle", "proj")
	ents, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(base, e.Name(), "skills"))
		}
	}
	return dirs, nil
}
