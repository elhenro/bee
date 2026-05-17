package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// cmdUsage tracks how often each slash command / skill is invoked so the
// palette can rank by frequency when the filter is empty. persisted to
// <beeHome>/cmd_usage.json (BEE_HOME wins, else ~/.bee). best-effort: i/o
// errors silently fall back to in-memory counts.
type cmdUsage struct {
	mu     sync.Mutex
	counts map[string]int
	path   string
}

func cmdUsagePath() string {
	home := os.Getenv("BEE_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".bee")
	}
	return filepath.Join(home, "cmd_usage.json")
}

func loadCmdUsage() *cmdUsage {
	u := &cmdUsage{counts: map[string]int{}, path: cmdUsagePath()}
	if u.path == "" {
		return u
	}
	b, err := os.ReadFile(u.path)
	if err != nil {
		return u
	}
	_ = json.Unmarshal(b, &u.counts)
	return u
}

func (u *cmdUsage) count(name string) int {
	if u == nil {
		return 0
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.counts[name]
}

// bump increments name's count and persists synchronously. Writing the
// tiny counts file is sub-millisecond; going async would race when several
// bumps fire back-to-back from one keystroke.
func (u *cmdUsage) bump(name string) {
	if u == nil || name == "" {
		return
	}
	u.mu.Lock()
	u.counts[name]++
	snapshot := make(map[string]int, len(u.counts))
	for k, v := range u.counts {
		snapshot[k] = v
	}
	path := u.path
	u.mu.Unlock()
	if path == "" {
		return
	}
	saveCmdUsage(path, snapshot)
}

func saveCmdUsage(path string, counts map[string]int) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(counts)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// sortPaletteByUsage orders entries by usage count desc, with alphabetical
// tiebreak so unused entries stay in a stable readable order.
func sortPaletteByUsage(pool []PaletteEntry, u *cmdUsage) {
	if len(pool) < 2 {
		return
	}
	sort.SliceStable(pool, func(i, j int) bool {
		ci, cj := u.count(pool[i].Name), u.count(pool[j].Name)
		if ci != cj {
			return ci > cj
		}
		return pool[i].Name < pool[j].Name
	})
}
