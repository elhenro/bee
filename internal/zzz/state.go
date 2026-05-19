package zzz

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HomeDir returns ~/.bee/zzz. Created on first call.
// BEE_HOME overrides ~/.bee (matches bgreg/agents and lets tests stay
// hermetic on Windows where os.UserHomeDir reads USERPROFILE, not HOME).
func HomeDir() (string, error) {
	root := os.Getenv("BEE_HOME")
	if root == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(h, ".bee")
	}
	d := filepath.Join(root, "zzz")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// RunDir returns ~/.bee/zzz/runs/<id>, creating it.
func RunDir(id string) (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, "runs", id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// WorktreeDir returns ~/.bee/zzz/worktrees/<id>. NOT created — git worktree
// add wants a non-existent path.
func WorktreeDir(id string) (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "worktrees", id), nil
}

// NewID returns a short timestamped id: 20260518-a1b2c3d4. Sortable, unique
// enough for human-facing dirs, no external deps. UUIDs exist elsewhere in
// bee but they read poorly in `ls` output for an overnight log dir.
func NewID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102") + "-" + hex.EncodeToString(b[:])
}

// SaveMeta writes meta.json atomically.
func SaveMeta(r *Run) error {
	dir, err := RunDir(r.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "meta.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "meta.json"))
}

// LoadMeta reads meta.json for an existing run.
func LoadMeta(id string) (*Run, error) {
	dir, err := RunDir(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	var r Run
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SavePrompt persists the original objective so resume keeps context.
func SavePrompt(id, objective string) error {
	dir, err := RunDir(id)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte(objective), 0o644)
}

// LoadPrompt reads the saved objective.
func LoadPrompt(id string) (string, error) {
	dir, err := RunDir(id)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// AppendEvent writes one line to events.jsonl.
func AppendEvent(id string, ev Event) error {
	dir, err := RunDir(id)
	if err != nil {
		return err
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// ListRuns returns all runs in ~/.bee/zzz/runs/, newest first.
func ListRuns() ([]*Run, error) {
	home, err := HomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, "runs")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Run
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := LoadMeta(e.Name())
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out, nil
}

// LatestRun returns the most recently started run, or nil.
func LatestRun() (*Run, error) {
	runs, err := ListRuns()
	if err != nil || len(runs) == 0 {
		return nil, err
	}
	return runs[0], nil
}

// Summary renders a one-line listing entry. Objective is truncated to keep
// columns aligned in `bee zzz --list` output.
func (r *Run) Summary() string {
	dur := r.EndedAt.Sub(r.StartedAt).Truncate(time.Second)
	if r.EndedAt.IsZero() {
		dur = time.Since(r.StartedAt).Truncate(time.Second)
	}
	obj := r.Objective
	if rs := []rune(obj); len(rs) > 80 {
		obj = string(rs[:77]) + "..."
	}
	return fmt.Sprintf("%s  %-10s  iter=%d  tok=%d/%d  $%.4f  %s  %s",
		r.ID, r.Status, r.IterCount, r.Tokens.Input, r.Tokens.Output, r.Tokens.USD, dur, obj)
}
