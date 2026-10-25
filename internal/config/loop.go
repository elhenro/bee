package config

import "time"

// watchdog defaults. used when the corresponding config field is zero.
const (
	// DefaultWatchdogTimeout is the headless per-attempt wall-clock cap.
	DefaultWatchdogTimeout = 10 * time.Minute
	// DefaultWatchdogStall is the TUI inactivity threshold before re-trigger.
	DefaultWatchdogStall = 90 * time.Second
	// DefaultWatchdogMaxResumes caps consecutive auto-resumes per task.
	DefaultWatchdogMaxResumes = 3
)

// MemoryConfig gates the heuristic memory loop. Enabled=false skips both
// scan and select on every turn.
type MemoryConfig struct {
	Enabled bool `toml:"enabled"`
	TopK    int  `toml:"top_k"`
	// LLMHints lets a thin deterministic lookup ask a side model for keyword
	// tags before the first request. Off by default: it puts a model round trip
	// in front of every turn's first token.
	LLMHints bool `toml:"llm_hints"`
}

// WaggleConfig gates procedure memory: crystallizing repeated read-only routes
// into runnable waggles, predictive replay, and the waggle_lookup tool. On by
// default; disable in config.toml with [waggle] enabled = false. Applies to
// every role (worker/scout/queen) and to queen-spawned hive workers.
type WaggleConfig struct {
	Enabled bool `toml:"enabled"`
}

// CompactionConfig controls auto-summarization of long histories. Threshold is
// the fraction of the total context budget at which the loop auto-compacts.
type CompactionConfig struct {
	Enabled   bool    `toml:"enabled"`
	Threshold float64 `toml:"threshold"` // 0.0–1.0
}

// WatchdogConfig is the auto-resume safety net. When a turn stops on a
// recoverable condition (timeout, dropped stream, transient wedge, max-iter),
// bee re-triggers the model with a continuation instead of giving up — bounded
// so it can never loop forever. Conservative by design: a long but *active*
// turn (reasoning or tool output streaming) is never interrupted — the TUI
// stall check keys off inactivity, not wall-clock duration.
type WatchdogConfig struct {
	Enabled bool `toml:"enabled"`
	// TimeoutSec is the headless per-attempt wall-clock cap. 0 → 600 (10m).
	TimeoutSec int `toml:"timeout_seconds"`
	// StallSeconds is the TUI inactivity threshold before re-trigger. 0 → 90.
	StallSeconds int `toml:"stall_seconds"`
	// MaxResumes caps consecutive auto-resumes per task. 0 → 3.
	MaxResumes int `toml:"max_resumes"`
}

// Timeout resolves the headless per-attempt wall-clock cap.
func (w WatchdogConfig) Timeout() time.Duration {
	if w.TimeoutSec <= 0 {
		return DefaultWatchdogTimeout
	}
	return time.Duration(w.TimeoutSec) * time.Second
}

// Stall resolves the TUI inactivity threshold.
func (w WatchdogConfig) Stall() time.Duration {
	if w.StallSeconds <= 0 {
		return DefaultWatchdogStall
	}
	return time.Duration(w.StallSeconds) * time.Second
}

// Resumes resolves the per-task auto-resume cap.
func (w WatchdogConfig) Resumes() int {
	if w.MaxResumes <= 0 {
		return DefaultWatchdogMaxResumes
	}
	return w.MaxResumes
}
