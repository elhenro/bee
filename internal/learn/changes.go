// Package learn builds the review aids shown after a turn: what the turn
// changed and why, from data the harness already holds. No model call, so it
// adds no latency and no tokens.
package learn

import (
	"regexp"
	"slices"
	"strings"

	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// Change is one file a turn mutated, with the reasons the model gave. The
// text is the model's own, untouched: strip it before printing.
type Change struct {
	Path string
	Whys []string
}

var patchPath = regexp.MustCompile(`(?m)^\+\+\+ (?:b/)?(\S+)`)

// turns splits msgs at each real user request; whatever precedes the first
// request is a turn of its own.
func turns(msgs []types.Message) [][]types.Message {
	var out [][]types.Message
	start := 0
	for i, m := range msgs {
		if i > start && isRequest(m) {
			out = append(out, msgs[start:i])
			start = i
		}
	}
	return append(out, msgs[start:])
}

// LastTurn is the messages since the latest real user request.
func LastTurn(msgs []types.Message) []types.Message {
	ts := turns(msgs)
	return ts[len(ts)-1]
}

// isRequest is a user message the user wrote, not one the loop injected.
func isRequest(m types.Message) bool {
	if m.Role != types.RoleUser || m.Ephemeral {
		return false
	}
	t := text(m)
	return t != "" && !types.IsSyntheticUserText(t)
}

// Touched lists the files msgs mutated, first-touched first. Paths are made
// relative to cwd when they sit under it.
func Touched(msgs []types.Message, cwd string) []Change {
	idx := map[string]int{}
	var out []Change
	add := func(path, reason string) {
		path = tools.RelTo(cwd, strings.TrimSpace(path))
		if path == "" || path == "/dev/null" {
			return
		}
		i, ok := idx[path]
		if !ok {
			i = len(out)
			idx[path] = i
			out = append(out, Change{Path: path})
		}
		if reason != "" && !slices.Contains(out[i].Whys, reason) {
			out[i].Whys = append(out[i].Whys, reason)
		}
	}
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type != types.BlockToolUse || c.Use == nil || !types.TakesWhy(c.Use.Name) {
				continue
			}
			reason := c.Use.Why()
			if c.Use.Name == "apply_patch" {
				patch, _ := c.Use.Input["patch"].(string)
				for _, hit := range patchPath.FindAllStringSubmatch(patch, -1) {
					add(hit[1], reason)
				}
				continue
			}
			path, _ := c.Use.Input["path"].(string)
			add(path, reason)
		}
	}
	return out
}
