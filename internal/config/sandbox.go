package config

// SandboxConfig is a two-axis sandbox policy.
//
// Scope: read-only | workspace-write | workspace-write-net | danger-full-access.
// Approval: untrusted | on-request | on-failure | never.
//
// CommandAllowlist holds safety.DangerousPattern keys the user has granted
// AllowAlways. Loaded into the approval.Cache at startup; appended via
// PersistSetting when the user picks AllowAlways at a prompt.
type SandboxConfig struct {
	Scope            string   `toml:"scope"`
	Approval         string   `toml:"approval"`
	CommandAllowlist []string `toml:"command_allowlist"`
	// AllowOutsideCwd lifts the default confinement of file tools to the
	// start directory. Off: a path outside it asks the user first.
	AllowOutsideCwd bool `toml:"allow_outside_cwd"`
}

// Confined reports whether an OS-confinement scope is active. The default
// danger-full-access scope (and unset) is unconfined: restriction layers like
// secret redaction gate on this so full-access runs see raw tool output.
func (s SandboxConfig) Confined() bool {
	return s.Scope != "" && s.Scope != "danger-full-access"
}

// ShellConfig controls how the bash tool launches commands. Default keeps
// the hermetic `bash -c` shape (no rc files). UseUserRC=true sources the
// user's interactive rc file before each command so aliases and functions
// from .zshrc / .bashrc become available. Tradeoff: leaks rc-banner output
// into tool results, slower startup, breaks reproducibility — opt in only.
type ShellConfig struct {
	UseUserRC bool   `toml:"use_user_rc"`
	Shell     string `toml:"shell"`   // override $SHELL (e.g. "/bin/zsh"); empty = autodetect
	RCFile    string `toml:"rc_file"` // override rc path; empty = .zshrc / .bashrc per shell
}
