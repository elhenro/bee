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

// CurateResult reports what a Curate pass changed, for the gc summary line.
type CurateResult struct {
	RemovedProj int
	RemovedUser int
	PrunedProj  int
	PrunedUser  int
	Demoted     int
	Promoted    int
}

// Curate runs the full library maintenance pass over both scopes, in the order
// that keeps each step's guarantee intact:
//  1. GC      — drop duplicate/broken files.
//  2. Demote  — disable chronic divergers (rewriting the file refreshes its
//     mtime) so the prune below keeps them this run.
//  3. Prune   — delete never-paid-off stale files; a just-demoted file survives
//     via its fresh mtime and only ages out on a later pass.
//  4. Compact — drop ledger history for files now gone, so a re-mined identical
//     route starts clean instead of inheriting a dead route's stats.
//  5. Promote — copy routes recurring across projects (skipping disabled) into
//     the user store.
//
// now is injected for deterministic tests.
func Curate(proj, user *Store, staleAge time.Duration, minFails int, now time.Time) (CurateResult, error) {
	var res CurateResult
	res.RemovedProj, _ = GC(proj)
	res.RemovedUser, _ = GC(user)
	ps, _ := ReadLedger(proj.LedgerPath())
	us, _ := ReadLedger(user.LedgerPath())
	dp, _ := Demote(proj, ps, minFails)
	du, _ := Demote(user, us, minFails)
	res.Demoted = dp + du
	res.PrunedProj, _ = PruneStale(proj, ps, staleAge, now)
	res.PrunedUser, _ = PruneStale(user, us, staleAge, now)
	// only compact when the skills dir was read cleanly: an empty keep set means
	// "delete all history", so a failed enumeration must NOT reach CompactLedger.
	if keep, err := survivingNames(proj); err == nil {
		_ = CompactLedger(proj.LedgerPath(), keep)
	}
	if keep, err := survivingNames(user); err == nil {
		_ = CompactLedger(user.LedgerPath(), keep)
	}
	res.Promoted, _ = Promote(user)
	return res, nil
}

// survivingNames is the set of waggle names whose file is still on disk. A
// missing dir is not an error (empty set, nil); any other read error propagates
// so Curate can skip ledger compaction rather than wipe a valid ledger.
func survivingNames(s *Store) (map[string]bool, error) {
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			keep[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}
	return keep, nil
}

// isDisabled reports whether a waggle file's frontmatter has disabled: true.
// Unreadable or unparseable files count as not disabled — other readers skip
// them on their own terms.
func isDisabled(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	fmBytes, ok := frontmatterBytes(raw)
	if !ok {
		return false
	}
	var meta struct {
		Disabled bool `yaml:"disabled"`
	}
	if yaml.Unmarshal(fmBytes, &meta) != nil {
		return false
	}
	return meta.Disabled
}

// Demote disables waggles whose predictive replay repeatedly diverged without
// ever paying off: at least minFails recorded divergences (per the ledger) and
// zero successful uses. A demoted waggle keeps its file (for inspection) but is
// marked disabled, so LoadRoutes skips it and replay stops firing a route the
// tree has outgrown. Idempotent: already-disabled waggles are not re-counted.
// Returns the number newly demoted.
func Demote(s *Store, stats map[string]Stat, minFails int) (int, error) {
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	demoted := 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		st := stats[name]
		if st.Uses > 0 || st.Fails < minFails {
			continue
		}
		if disableFile(filepath.Join(s.dir, e.Name())) == nil {
			demoted++
		}
	}
	return demoted, nil
}

// disableFile sets disabled: true in a waggle's frontmatter, preserving its
// route/exec metadata so the file stays a valid (if dormant) exec-skill. Returns
// a non-nil error when the file is already disabled, so Demote stays idempotent.
func disableFile(path string) error {
	raw, err := os.ReadFile(path)
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
	if fm.Disabled {
		return os.ErrExist // already disabled — don't recount
	}
	fm.Disabled = true
	y, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	body := "Crystallized read-only route. Disabled by curation (repeated replay divergence).\n"
	return os.WriteFile(path, []byte("---\n"+string(y)+"---\n"+body), 0o644)
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
			if isDisabled(p) {
				continue // a demoted route is not a promotion candidate
			}
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
	fm.Disabled = false // a promoted copy always starts enabled
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
