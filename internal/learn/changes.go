// Package learn builds the review aids shown after a turn: what the turn
// changed and why, from data the harness already holds. No model call, so it
// adds no latency and no tokens.
package learn

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/tools/why"
	"github.com/elhenro/bee/internal/types"
)

// Change is one file a turn mutated, with the reasons the model gave.
type Change struct {
	Path string
	Whys []string
}

var patchPath = regexp.MustCompile(`(?m)^\+\+\+ (?:b/)?(\S+)`)

// Touched lists the files mutated since the last user message, first-touched
// first. Paths are made relative to cwd when they sit under it.
func Touched(msgs []types.Message, cwd string) []Change {
	start := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleUser {
			start = i + 1
			break
		}
	}
	idx := map[string]int{}
	var out []Change
	add := func(path, reason string) {
		path = Clean(rel(cwd, path))
		reason = Clean(reason)
		if path == "" || path == "/dev/null" {
			return
		}
		i, ok := idx[path]
		if !ok {
			i = len(out)
			idx[path] = i
			out = append(out, Change{Path: path})
		}
		if reason != "" && !contains(out[i].Whys, reason) {
			out[i].Whys = append(out[i].Whys, reason)
		}
	}
	for _, m := range msgs[start:] {
		for _, c := range m.Content {
			if c.Type != types.BlockToolUse || c.Use == nil || !why.Mutators[c.Use.Name] {
				continue
			}
			reason := why.Of(c.Use.Input)
			if c.Use.Name == "apply_patch" {
				patch, _ := c.Use.Input["patch"].(string)
				for _, m := range patchPath.FindAllStringSubmatch(patch, -1) {
					add(m[1], reason)
				}
				continue
			}
			path, _ := c.Use.Input["path"].(string)
			add(path, reason)
		}
	}
	return out
}

func rel(cwd, p string) string {
	p = strings.TrimSpace(p)
	if cwd != "" && filepath.IsAbs(p) {
		if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Paths returns the sorted unique paths of cs.
func Paths(cs []Change) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Path
	}
	sort.Strings(out)
	return out
}
