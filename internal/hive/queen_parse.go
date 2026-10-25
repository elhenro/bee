package hive

import (
	"encoding/json"
	"strings"
)

// roleNamesCSV returns role names in stable order for prompt embedding.
func roleNamesCSV() []string {
	roles := AllRoles()
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// parseSubTasks extracts a JSON array from the planner output. Two shapes are
// tolerated:
//
//   - []string (legacy)             → each entry becomes a SubTask with RoleBuilder
//   - []{role, task} (new)          → role is parsed via ParseRole; unknown → RoleBuilder
//
// Fenced code blocks and prose wrappers are stripped before parsing.
func parseSubTasks(s string) []SubTask {
	body := extractArray(stripFence(s))
	if body == "" {
		return nil
	}

	var out []SubTask
	add := func(role, task string) {
		if task = strings.TrimSpace(task); task == "" {
			return
		}
		r, ok := ParseRole(role)
		if !ok {
			r = RoleBuilder
		}
		out = append(out, SubTask{Role: r, Task: task})
	}

	// try object form first
	var objs []struct {
		Role string `json:"role"`
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &objs); err == nil {
		for _, o := range objs {
			add(o.Role, o.Task)
		}
		if len(out) > 0 {
			return out
		}
	}

	// fallback: legacy string array
	var arr []string
	if err := json.Unmarshal([]byte(body), &arr); err == nil {
		for _, x := range arr {
			add("", x)
		}
	}
	return out
}

// extractArray returns the first [...] block in s, or "" when there is none.
func extractArray(s string) string {
	start := strings.IndexByte(s, '[')
	end := strings.LastIndexByte(s, ']')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// drop first line (fence + optional lang)
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return s
	}
	s = s[nl+1:]
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
