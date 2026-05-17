// Package bgreg is the per-session status sidecar registry for background
// bees. The bg engine writes; the agent-view TUI reads. One JSON file per
// session at <beeHome>/sessions/bg/<id>.status.json, written via temp+rename
// for atomic replacement.
package bgreg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type State string

const (
	StateActive   State = "active"
	StateAwaiting State = "awaiting"
	StateIdle     State = "idle"
	StateDone     State = "done"
	StateFailed   State = "failed"
)

type Status struct {
	SchemaV      int       `json:"schema_v"`
	SessionID    string    `json:"session_id"`
	PID          int       `json:"pid"`
	State        State     `json:"state"`
	Task         string    `json:"task"`
	LastResponse string    `json:"last_response"`
	Model        string    `json:"model"`
	Cwd          string    `json:"cwd"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const currentSchema = 1
const previewMax = 240
const envBeeHome = "BEE_HOME"

func beeHome() (string, error) {
	if v := os.Getenv(envBeeHome); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".bee"), nil
}

func dir() (string, error) {
	h, err := beeHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "sessions", "bg"), nil
}

func path(sessionID string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, sessionID+".status.json"), nil
}

func Truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= previewMax {
		return s
	}
	return s[:previewMax-1] + "…"
}

func Write(s Status) error {
	if s.SessionID == "" {
		return errors.New("bgreg.Write: empty SessionID")
	}
	s.SchemaV = currentSchema
	s.Task = Truncate(s.Task)
	s.LastResponse = Truncate(s.LastResponse)
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = time.Now().UTC()
	}
	p, err := path(s.SessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), s.SessionID+"-*.tmp")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&s); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func Read(sessionID string) (Status, error) {
	p, err := path(sessionID)
	if err != nil {
		return Status{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Status{}, err
	}
	var s Status
	if err := json.Unmarshal(b, &s); err != nil {
		return Status{}, err
	}
	return s, nil
}

func List() ([]Status, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Status
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".status.json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".status.json")
		s, err := Read(id)
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func Remove(sessionID string) error {
	p, err := path(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
