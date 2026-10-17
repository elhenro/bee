package config

import "time"

// SwarmConfig tunes unattended multi-bee work (`bee zzz`, `bee agents`, the
// hive): the verify gate work must pass before it lands, the load throttle on
// new workers, the retry budgets, and which model works vs reviews.
//
//	[swarm]
//	verify         = ["go build ./...", "go test ./..."]
//	max_load_ratio = 2.0
//	max_hops       = 3
//	max_resumes    = 3
//
//	[swarm.worker]
//	provider = "ollama"
//	model    = "qwen3:8b"
//
//	[swarm.reviewer]
//	provider = "anthropic"
//	model    = "claude-sonnet-4-5"
type SwarmConfig struct {
	// Verify is the fallback gate: shell commands that must all exit 0. A repo's
	// own .bee/verify file wins over it. Empty = no gate.
	Verify []string `toml:"verify"`
	// VerifyTimeoutSeconds bounds one verify command; 0 → 600.
	VerifyTimeoutSeconds int `toml:"verify_timeout_seconds"`
	// MaxLoadRatio holds new workers while the 1-minute load average per core
	// is above it; 0 disables the throttle.
	MaxLoadRatio float64 `toml:"max_load_ratio"`
	// MaxHops is how many red verifies one piece of work may take before it is
	// dropped (zzz) or blocked for a human (agents). Conflicts are free.
	MaxHops int `toml:"max_hops"`
	// MaxResumes caps how often a stopped agent is re-started without handing
	// in. A DONE resets the count.
	MaxResumes int `toml:"max_resumes"`
	// Worker and Reviewer route hive engines by tier: cheap model writes, a
	// stronger one reads the diff. Empty fields inherit the session default.
	Worker   ModelTier `toml:"worker"`
	Reviewer ModelTier `toml:"reviewer"`
}

// ModelTier names the provider/model for one hive role.
type ModelTier struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
}

// IsZero reports whether the tier leaves the session default in place.
func (t ModelTier) IsZero() bool { return t.Provider == "" && t.Model == "" }

// Apply returns cfg pointed at the tier's provider/model.
func (t ModelTier) Apply(cfg Config) Config {
	if t.Provider != "" {
		cfg.DefaultProvider = t.Provider
	}
	if t.Model != "" {
		cfg.DefaultModel = t.Model
	}
	return cfg
}

// VerifyTimeout resolves the per-command verify timeout.
func (s SwarmConfig) VerifyTimeout() time.Duration {
	if s.VerifyTimeoutSeconds > 0 {
		return time.Duration(s.VerifyTimeoutSeconds) * time.Second
	}
	return 600 * time.Second
}

// Hops resolves MaxHops (0 → 3).
func (s SwarmConfig) Hops() int {
	if s.MaxHops > 0 {
		return s.MaxHops
	}
	return 3
}

// Resumes resolves MaxResumes (0 → 3).
func (s SwarmConfig) Resumes() int {
	if s.MaxResumes > 0 {
		return s.MaxResumes
	}
	return 3
}
