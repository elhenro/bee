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

// MergeState tracks agent-mode branch lifecycle. Empty string for legacy
// bg sessions that don't run on a worktree.
type MergeState string

const (
	MergeStateNone     MergeState = ""
	MergeStateUnmerged MergeState = "unmerged"
	MergeStateMerging  MergeState = "merging"
	MergeStateMerged   MergeState = "merged"
	MergeStateConflict MergeState = "conflict"
)

type Status struct {
	SchemaV      int       `json:"schema_v"`
	Version      int       `json:"version"` // monotonic write-generation, used by Update CAS
	SessionID    string    `json:"session_id"`
	PID          int       `json:"pid"`
	State        State     `json:"state"`
	Task         string    `json:"task"`
	LastResponse string    `json:"last_response"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider,omitempty"`
	Cwd          string    `json:"cwd"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`

	// agent-mode fields. Empty / zero on legacy bg sessions.
	WorktreePath string     `json:"worktree_path,omitempty"`
	Branch       string     `json:"branch,omitempty"`
	MergeState   MergeState `json:"merge_state,omitempty"`
	ConflictMsg  string     `json:"conflict_msg,omitempty"`
	InputTokens  int        `json:"input_tokens,omitempty"`
	OutputTokens int        `json:"output_tokens,omitempty"`
}

const currentSchema = 2
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

// Write replaces the on-disk status atomically. Version is auto-incremented
// from the on-disk value (last-writer-wins). For race-free read-modify-write
// use Update instead — it serializes via a per-session lock and rejects stale
// updates.
func Write(s Status) error {
	if s.SessionID == "" {
		return errors.New("bgreg.Write: empty SessionID")
	}
	s.SchemaV = currentSchema
	if cur, err := Read(s.SessionID); err == nil && cur.Version >= s.Version {
		s.Version = cur.Version + 1
	} else if s.Version == 0 {
		s.Version = 1
	}
	return writeRaw(s)
}

// writeRaw skips the version-bump dance. Used internally by Update which
// already holds the lock and has computed the new version.
func writeRaw(s Status) error {
	if s.SessionID == "" {
		return errors.New("bgreg.writeRaw: empty SessionID")
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

// Update performs an atomic read-modify-write on the status for sessionID.
// fn receives a copy of the current Status; mutating fields and returning nil
// commits the change with a bumped Version. The status file is locked via a
// per-session sidecar (.lock) so concurrent Update calls — even across
// processes — serialize. fn returning a non-nil error aborts the write.
//
// ErrStaleUpdate is returned if the on-disk Version advanced while fn ran
// (only possible if fn took longer than the lock acquisition retry window or
// if a writer bypassed Update — Write keeps last-writer-wins semantics).
func Update(sessionID string, fn func(*Status) error) error {
	if sessionID == "" {
		return errors.New("bgreg.Update: empty sessionID")
	}
	lock, err := acquireSessionLock(sessionID)
	if err != nil {
		return err
	}
	defer lock.release()

	cur, rerr := Read(sessionID)
	if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return rerr
	}
	if cur.SessionID == "" {
		cur.SessionID = sessionID
	}
	before := cur.Version
	next := cur
	if err := fn(&next); err != nil {
		return err
	}

	verify, verr := Read(sessionID)
	if verr != nil && !errors.Is(verr, os.ErrNotExist) {
		return verr
	}
	if verify.Version != before {
		return ErrStaleUpdate
	}
	next.Version = before + 1
	next.UpdatedAt = time.Now().UTC()
	return writeRaw(next)
}

// ErrStaleUpdate signals that the status changed during an Update callback —
// indicates a writer bypassed the lock (used Write directly). Callers can
// retry Update.
var ErrStaleUpdate = errors.New("bgreg: stale update (version moved during fn)")

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
