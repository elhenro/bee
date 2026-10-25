package skills

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Registry holds the in-memory index of parsed skills.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Skill
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Skill)}
}

// Load scans dir for *.md files and (re)populates the registry.
// Parse errors are returned as a joined error but valid skills still load.
func (r *Registry) Load(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skills dir %s: %w", dir, err)
	}

	loaded := make(map[string]Skill)
	var errs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		s, err := ParseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		loaded[s.Name] = s
	}
	r.mu.Lock()
	r.byName = loaded
	r.mu.Unlock()
	if len(errs) > 0 {
		return fmt.Errorf("skill parse errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (r *Registry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byName[name]
	return s, ok
}

// List returns a copy of all skills sorted by name.
func (r *Registry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := slices.Collect(maps.Values(r.byName))
	slices.SortFunc(out, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Manifest returns a one-line-per-skill string for system-prompt injection.
// Format: `name: description (kind)`. Empty if no skills.
func (r *Registry) Manifest() string {
	var lines []string
	for _, s := range r.List() {
		// keep each line tight; system-prompt budget matters
		lines = append(lines, fmt.Sprintf("%s: %s (%s)", s.Name, cmp.Or(s.Description, "(no description)"), s.Kind))
	}
	return strings.Join(lines, "\n")
}
