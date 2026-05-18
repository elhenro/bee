// Package config holds the bee runtime configuration schema and loader.
//
// Layered resolution: Defaults() < ~/.bee/config.toml < env vars. Profiles
// (tiny/normal/large) overlay budgets onto the top-level fields so the rest
// of the codebase only ever reads flat values.
package config

// Config is the merged, post-resolution view of bee's settings. Adapters and
// the agent loop read from this; nothing else.
type Config struct {
	DefaultProvider string                    `toml:"default_provider"`
	DefaultModel    string                    `toml:"default_model"`
	Caveman         string                    `toml:"caveman"`
	Profile         string                    `toml:"profile"`
	Thinking        string                    `toml:"thinking"` // auto | off | low | medium | high
	// Mode gates how the agent reacts to user input. plan = read-only
	// research + proposed plan, no mutations. edit = full tool surface
	// (default). auto = side-LLM classifier picks plan|edit per turn.
	Mode string `toml:"mode"` // plan | auto | edit
	Sandbox         SandboxConfig             `toml:"sandbox"`
	Shell           ShellConfig               `toml:"shell"`
	Memory          MemoryConfig              `toml:"memory"`
	Compaction      CompactionConfig          `toml:"compaction"`
	Providers       map[string]ProviderConfig `toml:"providers"`
	Profiles        map[string]Profile        `toml:"profiles"`

	// APIKey is the resolved key for the active provider. Populated by Load
	// from os.Getenv(provider.EnvKey). Not persisted to disk.
	APIKey string `toml:"-"`

	// ShowBanner controls the ASCII/emoji bee logo printed at TUI startup
	// AND the braille intro animation. Toggle via /settings; takes effect
	// on next launch (intro is a one-shot startup animation).
	ShowBanner bool `toml:"show_banner"`

	// ShowLoader controls the braille "generating" animation shown while
	// the model is producing the next turn (pre-token loader + animated
	// caret). Default true. Toggle via /settings; persists across launches.
	ShowLoader bool `toml:"show_loader"`

	// MaxIterations caps tool-use rounds per Run. 0 = use loop default (50).
	// Raise for tool-heavy agents; lower to fail fast on runaway loops.
	MaxIterations int `toml:"max_iterations"`

	// Verbose unlocks full tool-output rendering in the TUI (compact one-line
	// preview otherwise). Toggle via /settings; persists across launches.
	Verbose bool `toml:"verbose"`

	// ShowThoughts controls whether BlockThinking chain-of-thought is rendered
	// in scrollback. Default true. Toggle via /settings; persists across launches.
	ShowThoughts bool `toml:"show_thoughts"`

	// ShowNudges controls whether synthetic `[nudge]` user messages — emitted
	// by the loop's reasoning-only stall recovery — appear in the transcript.
	// Default false (hidden). Toggle via /settings; persists across launches.
	// Loop behavior is unaffected; this is a render-time filter only.
	ShowNudges bool `toml:"show_nudges"`

	// ShowRecap enables a one-line post-turn recap synthesized by a cheap
	// side-LLM call against the freshly finished assistant message. Off by
	// default (extra tokens, off-fast-path). Toggle via /settings; persists
	// across launches. Disabled = no generation, no render.
	ShowRecap bool `toml:"show_recap"`

	// Compact strips the spacing layer from the TUI (no outer gutter, no
	// blank line between turns, no user bg-tint, no OSC 133 prompt zones).
	// Default false = clean mode. Set true on small terminals or for a
	// denser layout. Toggle via /settings; persists across launches.
	Compact bool `toml:"compact"`

	// ShowContextBar reveals the thin context-fill progress strip pinned to
	// the bottom edge. Default false because the bee-glyph hex fill in the
	// top bar already conveys window utilisation; the bottom strip just adds
	// a row of visual noise. Toggle via /settings; persists across launches.
	ShowContextBar bool `toml:"show_context_bar"`

	// Highlight gates syntax highlighting (chroma) on tool output, file
	// content, edit/write diffs, and bash command summaries. Default true.
	// Toggle via /settings; persists across launches.
	Highlight bool `toml:"highlight"`

	// ShellBangSilent flips the behavior of the inline shell prefix `!`.
	// Default true = `!cmd` runs locally and the output is NOT forwarded to
	// the LLM (silent). false restores the legacy behavior where `!cmd`
	// appends its output to a user turn. `!!cmd` always runs in the
	// opposite mode (escape hatch). Toggle via /settings.
	ShellBangSilent bool `toml:"shell_bang_silent"`

	// Top-bar chrome toggles. Default true for each preserves the original
	// status-line look; flipping all five off collapses the entire top row.
	// Toggle via /settings; persists across launches.
	ShowBee         bool `toml:"show_bee"`          // 🐝 glyph
	ShowContextPct  bool `toml:"show_context_pct"`  // "4%" next to glyph
	ShowModel       bool `toml:"show_model"`        // provider/model label
	ShowCwd         bool `toml:"show_cwd"`          // current working dir
	ShowEffort      bool `toml:"show_effort"`       // "t:max" thinking level
	ShowTurnTimer   bool `toml:"show_turn_timer"`   // ⏱ live / final elapsed
	ShowGitBranch   bool `toml:"show_git_branch"`   // ⎇ current git branch
	ShowTotalTokens bool `toml:"show_total_tokens"` // Σ session tokens (in+out)

	// ExtraTools opts specific tools into the manifest beyond the active
	// profile's allowlist. Names match tool Spec().Name (e.g. "apply_patch",
	// "hashline_edit"). The default keeps the surface minimal; this is the
	// escape hatch for power users who want expert-mode mutators without
	// bumping to the `large` profile.
	ExtraTools []string `toml:"extra_tools"`

	// DisabledTools hides specific tools from the model regardless of profile
	// allowlists. Toggled via /tools. Names match tool Spec().Name.
	DisabledTools []string `toml:"disabled_tools"`

	// UserTools are caller-defined shell-alias tools. Each entry registers a
	// new tool whose Run executes a fixed bash command. Added via `/tools add`
	// and persisted to ~/.bee/config.toml.
	UserTools []UserTool `toml:"user_tools"`

	// UpdateCheck gates the hourly upstream-update probe.
	//   "ask"  — probe and surface a modal when main has new commits (default)
	//   "auto" — probe and apply silently (notice surfaces via warn line)
	//   "off"  — no probe, no prompt
	// Picked from the modal's four buttons; persists across launches.
	UpdateCheck string `toml:"update_check"`

	// UpdateRepo lets forks point the probe at a different GitHub repo.
	// Empty = elhenro/bee.
	UpdateRepo string `toml:"update_repo"`

	// UpdateBranch is the branch to compare against. Empty = main.
	UpdateBranch string `toml:"update_branch"`
}

// UserTool describes a custom shell-alias tool. Name becomes the tool id the
// model sees; Command is the bash command that runs on invocation. Optional
// arg fields let the model pass dynamic input appended to Command.
type UserTool struct {
	Name        string `toml:"name"`
	Command     string `toml:"command"`
	Description string `toml:"description"`
}

// ProviderConfig pairs a base_url with a wire_api selector. One adapter
// per wire format handles every OpenAI-compatible service.
type ProviderConfig struct {
	BaseURL      string       `toml:"base_url"`
	WireAPI      string       `toml:"wire_api"`
	EnvKey       string       `toml:"env_key"`
	DefaultModel string       `toml:"default_model"`
	OAuth        *OAuthConfig `toml:"oauth"` // optional; enables /login <provider>
	// KeyOptional marks providers whose api key is optional (e.g. omlx run
	// locally with admin-panel "skip localhost auth" enabled). When true,
	// Load won't error if EnvKey is set but unresolved and no key file is
	// stored — bee proceeds without an Authorization header.
	KeyOptional bool `toml:"key_optional"`
}

// OAuthConfig configures a generic OAuth 2.0 PKCE flow for a provider. bee
// does not ship preconfigured client_ids for any vendor — users supply their
// own under [providers.<name>.oauth].
type OAuthConfig struct {
	ClientID          string `toml:"client_id"`
	AuthorizeEndpoint string `toml:"authorize_endpoint"`
	TokenEndpoint     string `toml:"token_endpoint"`
	Scope             string `toml:"scope"`
	// RedirectPath defaults to /callback when empty.
	RedirectPath string `toml:"redirect_path"`
	// RedirectPort pins the loopback to a fixed port. Some providers only
	// allow an EXACT redirect_uri match so a random port fails authorize.
	// 0 = random.
	RedirectPort int `toml:"redirect_port"`
	// ExtraAuthorizeParams are added to the authorize URL verbatim. Use for
	// vendor-specific knobs like audience, prompt, id_token_hint.
	ExtraAuthorizeParams map[string]string `toml:"extra_authorize_params"`
	// AccountIDHeader, when set, injects the per-account id from the saved
	// token's id_token claim into this header on every request. ChatGPT
	// backend requires "chatgpt-account-id".
	AccountIDHeader string `toml:"account_id_header"`
	// AccountIDClaim is the JWT claim name in id_token that holds the account
	// id. Default for openai is "https://api.openai.com/auth" -> ".chatgpt_account_id".
	AccountIDClaim string `toml:"account_id_claim"`
}

// Profile carries the small-model-friendly context budgets defined in
// PLAN §6b. Profile values are applied onto Config top-level fields by
// ApplyProfile.
type Profile struct {
	SystemPromptBudget       int    `toml:"system_prompt_budget"`
	MemoryTopK               int    `toml:"memory_top_k"`
	MemoryBodyChars          int    `toml:"memory_body_chars"` // per-memory body cap; 0 → unbounded
	ToolDescChars            int    `toml:"tool_desc_chars"`
	SkillManifestChars       int    `toml:"skill_manifest_chars"`
	Caveman                  string `toml:"caveman"`
	MaxIterations            int    `toml:"max_iterations"`              // iter cap override; 0 → use cfg.MaxIterations or loop default
	NoMutationStallThreshold int    `toml:"no_mutation_stall_threshold"` // streak threshold for stall warning; 0 → off (opt-in)
	// ToolFormat selects how tools are advertised. "" = native tool_calls
	// channel (default); "xml" = wrap inner provider with TextModeProvider
	// to inject a text-mode advert + parse `<name>{...}</name>` from the
	// assistant stream. Opt-in for local/tiny models that ignore tool_calls.
	ToolFormat string `toml:"tool_format"`
	// ToolOutputTokens caps a single tool-result payload in token estimates
	// (chars/4). 0 → fall back to per-tool default in internal/tools. Tiny
	// profile uses a much smaller cap so one fat `read` doesn't blow a 4-8k
	// local-model context in a single turn.
	ToolOutputTokens int `toml:"tool_output_tokens"`
	// SkipApplyPatch removes the apply_patch tool from the manifest. Tiny
	// models mis-emit unified diffs (wrong context lines, off-by-one hunks);
	// edit_diff + hashline_edit + write cover the same mutation surface
	// with shapes small models hit reliably.
	SkipApplyPatch bool `toml:"skip_apply_patch"`
	// ReadDefaultLines is the default line slice when the model omits `lines`.
	// 0 → tool's own default. Tiny uses 100 to stop one read torching the window.
	ReadDefaultLines int `toml:"read_default_lines"`
	// ReadMaxLines caps the largest slice a single read can return. 0 → tool default.
	ReadMaxLines int `toml:"read_max_lines"`
	// GrepMaxMatches caps grep result count per call. 0 → tool default.
	GrepMaxMatches int `toml:"grep_max_matches"`
	// Temperature / TopP pin sampling for this profile. Zero values mean
	// "use provider default". Tiny pins temperature=0 (deterministic tool
	// turns) with top_p=0.8 to keep 4-bit MoE outputs anchored.
	Temperature float64 `toml:"temperature"`
	TopP        float64 `toml:"top_p"`
	// ShowRecap, when non-nil, overrides Config.ShowRecap. Tiny sets false
	// so the side-LLM recap round-trip doesn't double turn latency on slow
	// local runs.
	ShowRecap *bool `toml:"show_recap"`
}

// boolPtr is the canonical helper for the optional bool overrides in Profile.
func boolPtr(b bool) *bool { return &b }

// SandboxConfig is a two-axis sandbox policy.
//
// Scope: read-only | workspace-write | danger-full-access.
// Approval: untrusted | on-request | on-failure | never.
//
// CommandAllowlist holds safety.DangerousPattern keys the user has granted
// AllowAlways. Loaded into the approval.Cache at startup; appended via
// PersistSetting when the user picks AllowAlways at a prompt.
type SandboxConfig struct {
	Scope            string   `toml:"scope"`
	Approval         string   `toml:"approval"`
	CommandAllowlist []string `toml:"command_allowlist"`
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

// MemoryConfig gates the heuristic memory loop. Enabled=false skips both
// scan and select on every turn.
type MemoryConfig struct {
	Enabled             bool `toml:"enabled"`
	TopK                int  `toml:"top_k"`
	BackgroundExtractor bool `toml:"background_extractor"`
}

// CompactionConfig controls auto-summarization of long histories. Threshold is
// the fraction of the total context budget at which the loop auto-compacts.
type CompactionConfig struct {
	Enabled   bool    `toml:"enabled"`
	Threshold float64 `toml:"threshold"` // 0.0–1.0
}
