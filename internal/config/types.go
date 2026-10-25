// Package config holds the bee runtime configuration schema and loader.
//
// Layered resolution: Defaults() < ~/.bee/config.toml < env vars. The rest of
// the codebase only ever reads the flat values.
package config

// DefaultMaxIterations is the default safety cap for tool-use rounds.
const DefaultMaxIterations = 100

// Config is the merged, post-resolution view of bee's settings. Adapters and
// the agent loop read from this; nothing else.
type Config struct {
	DefaultProvider string `toml:"default_provider"`
	DefaultModel    string `toml:"default_model"`
	// FastModel is a cheap model for side evals like /goal completion checks;
	// empty => use default_model.
	FastModel string `toml:"fast_model"`
	Caveman   string `toml:"caveman"`
	// Thinking is the reasoning budget. Empty = derive from Role per turn
	// (worker→auto, scout→high, queen→max). A non-empty value (set via
	// --thinking/--effort/BEE_EFFORT) is an explicit override that wins.
	Thinking string `toml:"thinking"` // "" | auto | off | low | medium | high | max
	// Role is the agent stance, cycled with shift+tab. worker = full surface
	// with a per-turn read|act classifier (default); scout = read-only research
	// + web; queen = spawns a hive. Each role bundles tool surface + reasoning
	// budget + orchestration.
	Role string `toml:"role"` // worker | scout | queen
	// Yolo auto-approves dangerous shell commands without prompting (a separate
	// safety toggle on any role; alt+y in the TUI, --yolo/--yes headless).
	Yolo bool `toml:"yolo"`
	// Mode is the legacy permission axis (plan/auto/edit/yolo), superseded by
	// Role. Kept only so an old config.toml migrates forward on load — not read
	// by the engine. See migrateLegacyRoleFields.
	Mode       string                    `toml:"mode"` // deprecated → Role
	Sandbox    SandboxConfig             `toml:"sandbox"`
	Shell      ShellConfig               `toml:"shell"`
	Memory     MemoryConfig              `toml:"memory"`
	Compaction CompactionConfig          `toml:"compaction"`
	Watchdog   WatchdogConfig            `toml:"watchdog"`
	Waggle     WaggleConfig              `toml:"waggle"`
	TUI        TUIConfig                 `toml:"tui"`
	Cron       CronConfig                `toml:"cron"`
	Swarm      SwarmConfig               `toml:"swarm"`
	Providers  map[string]ProviderConfig `toml:"providers"`

	// APIKey is the resolved key for the active provider. Populated by Load
	// from os.Getenv(provider.EnvKey). Not persisted to disk.
	APIKey string `toml:"-"`

	// ShowBanner controls the ASCII/emoji bee logo printed at TUI startup
	// AND the braille intro animation. Toggle via /settings; takes effect
	// on next launch (intro is a one-shot startup animation).
	ShowBanner bool `toml:"show_banner"`

	// ShowLoader controls the motion of the busy indicator while the model
	// is producing the next turn: the turning glyph on the spinner row, or
	// the braille animation of a pinned tui.loader. Off leaves a static bee
	// mark. Default true. Toggle via /settings; persists across launches.
	ShowLoader bool `toml:"show_loader"`

	// Figures shown on the busy row (and in the strip loader's readout).
	// Default true for in/out, false for rate.
	ShowLoaderIn   bool `toml:"show_loader_in"`   // ↑ input tokens
	ShowLoaderOut  bool `toml:"show_loader_out"`  // ↓ output tokens
	ShowLoaderRate bool `toml:"show_loader_rate"` // avg tok/s throughput

	// TutorialDone records that the user has finished or dismissed the
	// first-run interactive walkthrough. false (default) shows the welcome
	// gate on the next TUI start; set true to suppress it. Replay anytime
	// with /tutorial. Persisted by the tutorial flow, not /settings.
	TutorialDone bool `toml:"tutorial_done"`

	// MaxIterations caps tool-use rounds per Run. 0 = unlimited (loop until the
	// token-budget or read-only-stall guard fires). Defaults() seeds DefaultMaxIterations;
	// raise for tool-heavy agents, set 0 to lift the cap, lower to fail fast.
	MaxIterations int `toml:"max_iterations"`

	// SystemPromptBudget caps the assembled system prompt in estimated tokens
	// (chars/4): over it, memories, then skills, then context files are dropped.
	// 0 = unbounded.
	SystemPromptBudget int `toml:"system_prompt_budget"`
	// MemoryBodyChars caps each injected memory body; 0 = unbounded.
	MemoryBodyChars int `toml:"memory_body_chars"`
	// ToolDescChars caps each tool's line in the system prompt manifest.
	ToolDescChars int `toml:"tool_desc_chars"`
	// SkillManifestChars caps the skills section; 0 = unbounded, -1 = omit.
	SkillManifestChars int `toml:"skill_manifest_chars"`
	// ToolOutputTokens caps one tool result in estimated tokens (chars/4);
	// 0 = each tool's own default.
	ToolOutputTokens int `toml:"tool_output_tokens"`
	// ToolFormat selects how tools are advertised. "" = auto: local providers
	// use "json", hosted ones native tool_calls. "json" = grammar-constrained
	// JSON (response_format json_schema) for servers that compile it; "xml" =
	// text advert + `<name>{...}</name>` parsing for models that ignore native
	// tool_calls; "native" forces tool_calls everywhere.
	ToolFormat string `toml:"tool_format"`
	// StateCard replaces the rolling transcript in each LLM request with a
	// harness-maintained state card (goal, files touched, recent actions,
	// last error) plus the last few raw messages, and skips auto-compact. The
	// on-disk session keeps the full history. Opt-in.
	StateCard bool `toml:"state_card"`

	// MaxOutputTokens caps one model response (thinking included). 0 = auto:
	// local providers get LocalMaxOutputTokens, hosted ones keep their own
	// default. Negative lifts the cap. Raise it for very long single writes.
	MaxOutputTokens int `toml:"max_output_tokens"`

	// Mastermind is the legacy flag for hive orchestration, superseded by the
	// queen role. Kept only so an old config.toml migrates forward on load
	// (mastermind=true → role=queen). Not read by the engine.
	Mastermind bool `toml:"mastermind"` // deprecated → Role=queen

	// MastermindWorkers is how many worker bees the queen hive spawns per turn.
	// Defaults() seeds 3. The planner + critic are separate clones on top.
	MastermindWorkers int `toml:"mastermind_workers"`

	// MastermindReviewers is how many reviewer engines the queen hive spawns per
	// turn. Defaults() seeds 1. The review gate scores three dimensions
	// sequentially and Engine.Run is stateless across calls, so one engine
	// serves all three via the queen's round-robin. Raise only to parallelize
	// dimensions across distinct engines.
	MastermindReviewers int `toml:"mastermind_reviewers"`

	// MastermindTriage gates queen mode's adaptive routing: when true (default),
	// a cheap classifier decides whether a task is small/clear enough to handle
	// in a single streaming pass instead of spawning the full planning+review
	// hive. Set false to force the hive on every queen turn.
	MastermindTriage bool `toml:"mastermind_triage"`

	// MastermindParallel runs mutating workers concurrently, each in its own
	// git worktree, merging their changes back to the working tree as they
	// finish. Read-only workers always run in parallel in the shared tree.
	// When false (or outside a git repo) mutating workers serialize so they
	// never race the shared tree. Defaults() seeds true.
	MastermindParallel bool `toml:"mastermind_parallel"`

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
	// side-LLM call against the freshly finished assistant message. On by
	// default (off the fast path, a few tokens per turn). Toggle via /settings; persists
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

	// ShowCallDetail prints the whole call (full command, pattern, url) under
	// a tool call card that carries a model-written label. Default false: the
	// label alone. Failed calls always show it. Toggle via /settings; persists.
	ShowCallDetail bool `toml:"show_call_detail"`

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

	// Editor is the command launched by ctrl+o and /edit. Empty resolves at
	// runtime: $VISUAL, then $EDITOR, then vim.
	Editor string `toml:"editor"`

	// Top-bar chrome toggles. Default true for each preserves the original
	// status-line look; flipping all five off collapses the entire top row.
	// Toggle via /settings; persists across launches.
	ShowBee         bool `toml:"show_bee"`          // 🐝 glyph
	ShowContextPct  bool `toml:"show_context_pct"`  // "4%" next to glyph
	ShowModel       bool `toml:"show_model"`        // provider/model label
	ShowCwd         bool `toml:"show_cwd"`          // current working dir
	ShowEffort      bool `toml:"show_effort"`       // role chip (worker/scout/queen)
	ShowTurnTimer   bool `toml:"show_turn_timer"`   // ⏱ live / final elapsed
	ShowGitBranch   bool `toml:"show_git_branch"`   // ⎇ current git branch
	ShowTotalTokens bool `toml:"show_total_tokens"` // Σ session tokens (in+out)

	// OuterPadLR inserts N blank cells of left+right padding around the live
	// TUI region (top bar, streaming partial, input, modals). 0 = off. Capped
	// at 8 so a small terminal never collapses to a sliver. Finalized messages
	// flushed to terminal scrollback via tea.Println are NOT padded — the
	// setting only wraps the View() output. Toggle via /settings
	// `outer_pad_lr <n>`; persists across launches.
	OuterPadLR int `toml:"outer_pad_lr"`

	// DisabledTools hides specific tools from the model. Toggled via /tools.
	// Names match tool Spec().Name.
	DisabledTools []string `toml:"disabled_tools"`

	// UserTools are caller-defined shell-alias tools. Each entry registers a
	// new tool whose Run executes a fixed bash command. Added via `/tools add`
	// and persisted to ~/.bee/config.toml.
	UserTools []UserTool `toml:"user_tools"`

	// Browser gates the native chromedp-backed browser tools.
	Browser BrowserConfig `toml:"browser"`

	// Vision configures a fallback multimodal model. When the main model lacks
	// vision, the loop routes image blocks to this model, gets text back, and
	// injects it so blind models can still work with screenshots/pasted images.
	Vision VisionConfig `toml:"vision"`

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

// TUIConfig holds interactive-interface layout knobs. Kept separate from the
// flat top-level chrome toggles so ordered layout data (status_line) stays
// grouped and forward-compatible with machine-readable status modes.
type TUIConfig struct {
	// StatusLine is the ordered list of status-line item ids to render.
	// Item ids are owned by the tui package's item registry. An omitted key
	// (nil) uses the built-in default order; an explicit empty array hides
	// the line. Unknown ids are ignored and duplicates keep the first
	// occurrence, so a damaged list degrades gracefully instead of blocking
	// startup. Toggled from config only — machine-readable mode is opt-in.
	StatusLine []string `toml:"status_line"`

	// Loader picks the busy indicator while a turn runs. Empty or "spinner"
	// (default) is one row with the clock and token figures; "strip" is the
	// braille token stream, "phased" the pipeline that evolves with the wait,
	// and a painter name (wave, comet, hex, ...) pins that animation. The
	// BEE_LOADER environment variable overrides it.
	Loader string `toml:"loader"`
}

// UserTool describes a custom shell-alias tool. Name becomes the tool id the
// model sees; Command is the bash command that runs on invocation. Optional
// arg fields let the model pass dynamic input appended to Command.
type UserTool struct {
	Name        string `toml:"name"`
	Command     string `toml:"command"`
	Description string `toml:"description"`
}
