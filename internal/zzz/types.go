// Package zzz drives an autonomous overnight loop: clean-git-check →
// engine.Run → if tree changed, ONE commit; on any failure, git reset
// --hard back to last known good. Inspired by gnhf
// (github.com/kunchenguid/gnhf). Wakes you up to a branch full of
// individually-revertable commits and a notes.md log of every step.
package zzz

import "time"

// Config is the parsed flag set for one run.
type Config struct {
	Objective     string
	MaxIterations int
	MaxTokens     int    // 0 = unlimited
	StopWhen      string // substring match against FinalText
	Worktree      bool
	CurrentBranch bool // commit on current branch, no auto-create
	Push          bool // git push after each commit
	Sign          bool // default false (gnhf parity, avoids overnight prompts)
	NoVerify      bool // skip pre-commit hooks (opt-in)
}

// Run is the persisted metadata for one overnight session.
type Run struct {
	ID        string    `json:"id"`
	Objective string    `json:"objective"`
	Branch    string    `json:"branch"`
	Worktree  string    `json:"worktree,omitempty"`
	Mode      string    `json:"mode"` // "branch" | "current" | "worktree"
	RepoRoot  string    `json:"repo_root"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Status    string    `json:"status"` // "running" | "completed" | "failed" | "aborted"
	IterCount int       `json:"iter_count"`
	Tokens    TokenStat `json:"tokens"`
	Commits   []string  `json:"commits"`
	StopCause string    `json:"stop_cause,omitempty"`
}

// TokenStat is the running tally across all iterations.
type TokenStat struct {
	Input  int     `json:"input"`
	Output int     `json:"output"`
	USD    float64 `json:"usd"`
}

// IterationResult summarises one pass through the loop.
type IterationResult struct {
	Iter      int
	Status    string // "committed" | "reset" | "failed" | "noop"
	Subject   string
	CommitSHA string
	Tokens    TokenStat
	DiffStat  string
	Err       error
	Duration  time.Duration
}

// Event is one row in events.jsonl.
type Event struct {
	Iter     int       `json:"iter"`
	Phase    string    `json:"phase"`
	Time     time.Time `json:"time"`
	Tokens   TokenStat `json:"tokens,omitempty"`
	DiffStat string    `json:"diff_stat,omitempty"`
	Commit   string    `json:"commit,omitempty"`
	Err      string    `json:"err,omitempty"`
}

const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusAborted   = "aborted"

	IterCommitted = "committed"
	IterReset     = "reset"
	IterFailed    = "failed"
	IterNoop      = "noop"

	ModeBranch   = "branch"
	ModeCurrent  = "current"
	ModeWorktree = "worktree"
)
