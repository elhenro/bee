package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Registry holds the in-memory index of parsed skills.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Skill
	byPath map[string]string // path -> name (for watcher remove)
}

func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]Skill),
		byPath: make(map[string]string),
	}
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

	r.mu.Lock()
	r.byName = make(map[string]Skill)
	r.byPath = make(map[string]string)
	r.mu.Unlock()

	var errs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		s, err := ParseFile(p)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		r.put(s)
	}
	if len(errs) > 0 {
		return fmt.Errorf("skill parse errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Upsert parses one path and stores it. Used by the watcher.
func (r *Registry) Upsert(path string) (Skill, error) {
	s, err := ParseFile(path)
	if err != nil {
		// if the path was previously loaded under another name, drop the stale
		// entry so a broken edit doesn't leave a ghost.
		r.RemovePath(path)
		return Skill{}, err
	}
	r.put(s)
	return s, nil
}

// RemovePath drops whatever skill was associated with this file path.
func (r *Registry) RemovePath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name, ok := r.byPath[path]; ok {
		delete(r.byName, name)
		delete(r.byPath, path)
	}
}

func (r *Registry) put(s Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// if the name already maps to a different path, evict the old path entry
	if prev, ok := r.byName[s.Name]; ok && prev.Path != s.Path {
		delete(r.byPath, prev.Path)
	}
	r.byName[s.Name] = s
	r.byPath[s.Path] = s.Name
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
	out := make([]Skill, 0, len(r.byName))
	for _, s := range r.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Manifest returns a one-line-per-skill string for system-prompt injection.
// Format: `name: description (kind)`. Empty if no skills.
func (r *Registry) Manifest() string {
	skills := r.List()
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range skills {
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		// keep each line tight; system-prompt budget matters
		fmt.Fprintf(&b, "%s: %s (%s)\n", s.Name, desc, s.Kind)
	}
	return strings.TrimRight(b.String(), "\n")
}
