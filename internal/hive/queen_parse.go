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
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = stripFence(s)
	body := extractArray(s)
	if body == "" {
		return nil
	}

	// try object form first
	var objs []struct {
		Role string `json:"role"`
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &objs); err == nil && len(objs) > 0 {
		out := make([]SubTask, 0, len(objs))
		for _, o := range objs {
			task := strings.TrimSpace(o.Task)
			if task == "" {
				continue
			}
			role, ok := ParseRole(o.Role)
			if !ok {
				role = RoleBuilder
			}
			out = append(out, SubTask{Role: role, Task: task})
		}
		if len(out) > 0 {
			return out
		}
	}

	// fallback: legacy string array
	var arr []string
	if err := json.Unmarshal([]byte(body), &arr); err == nil {
		out := make([]SubTask, 0, len(arr))
		for _, x := range arr {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			out = append(out, SubTask{Role: RoleBuilder, Task: x})
		}
		return out
	}
	return nil
}

// extractArray returns the first [...] block in s, or s itself if it already
// parses as JSON. Empty string means no array found.
func extractArray(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return s
	}
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
