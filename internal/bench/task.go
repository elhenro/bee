// Package bench measures how a model+config combination behaves when driving
// bee through real coding tasks. It runs each task through the real binary's
// headless /goal loop, scores success/efficiency/format, and emits a JSON
// scoreboard. It never mutates bee config — improving bee is the caller's job.
package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Task is one benchmark scenario.
type Task struct {
	ID     string  `json:"id"`
	Prompt string  `json:"prompt"`           // goal condition handed to bee as "/goal <prompt>"
	Setup  string  `json:"setup,omitempty"`  // optional shell to scaffold $SANDBOX
	Checks []Check `json:"checks,omitempty"` // objective truth; empty falls back to Judge
	Judge  string  `json:"judge,omitempty"`  // LLM-judge condition when checks can't capture success
	Budget Budget  `json:"budget"`
}

// Check is one objective success assertion run against the sandbox.
type Check struct {
	Kind       string `json:"kind"`                  // "cmd" | "grep"
	Run        string `json:"run,omitempty"`         // cmd: shell line, $SANDBOX expanded
	ExpectExit int    `json:"expect_exit,omitempty"` // cmd: required exit code
	File       string `json:"file,omitempty"`        // grep: target file, $SANDBOX expanded
	Pattern    string `json:"pattern,omitempty"`     // grep: regex that must match
}

// Budget caps the goal loop and doubles as the denominator for efficiency.
type Budget struct {
	MaxTurns  int `json:"max_turns"`
	MaxTokens int `json:"max_tokens"`
}

// LoadTasks reads every *.json task spec in dir, sorted by id for stable runs.
func LoadTasks(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read suite dir: %w", err)
	}
	var tasks []Task
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var t Task
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		if t.ID == "" {
			return nil, fmt.Errorf("%s: task missing id", e.Name())
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}
