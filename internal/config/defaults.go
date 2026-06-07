package config

// Defaults returns the canonical out-of-the-box configuration: OpenRouter +
// deepseek-v4-flash, three profiles, caveman-full, danger-full-access +
// on-request sandbox, memory enabled with top_k=3.
//
// Zero-config startup: set OPENROUTER_API_KEY and bee runs.
func Defaults() Config {
	return Config{
		DefaultProvider: "openrouter",
		DefaultModel:    "deepseek/deepseek-v4-flash",
		// "auto" defers to the active profile's Caveman (tiny → full, normal →
		// full, large → lite). Disable per-run with `--caveman off` or in
		// config.toml: caveman = "off".
		Caveman: "auto",
		// "auto" picks tiny/normal/large by model class. Small/fast models
		// (flash/mini/nano/haiku/8b…) get the 4-tool tiny surface — a
		// minimal budget tuned for that class.
		Profile: "auto",
		// "" = derive from Role per turn (worker→auto, scout→high, queen→max).
		// Override per-run with `--thinking off|low|medium|high|max` (or "auto").
		Thinking: "",
		// "" lets migrateLegacyRoleFields resolve the role on load — from a
		// legacy mode= key if present, else worker. worker is the everyday role:
		// full tool surface with a per-turn 8-token classifier picking read|act,
		// so small models (flash/mini/8b…) don't reflex into shell on greetings.
		Role: "",
		Yolo: false,
		Sandbox: SandboxConfig{
			// danger-full-access: no OS confinement — the default. The seatbelt/bwrap
			// wrapper caused more friction than it prevented (blocked signals to
			// child processes, port binds, daemons) so confinement is opt-in now.
			// Re-enable it with `--safe` (writes confined to cwd+tmp, network
			// blocked) or `--sandbox read-only|workspace-write|workspace-write-net`.
			Scope:    "danger-full-access",
			Approval: "on-request",
		},
		Memory: MemoryConfig{
			Enabled:             true,
			TopK:                3,
			BackgroundExtractor: false,
		},
		Compaction: CompactionConfig{
			Enabled:   true,
			Threshold: 0.75,
		},
		// on by default with bounded resumes: a stopped turn re-triggers so
		// tasks finish unattended, but at most MaxResumes times per task.
		Watchdog: WatchdogConfig{
			Enabled:      true,
			TimeoutSec:   600,
			StallSeconds: 90,
			MaxResumes:   3,
		},
		// procedure memory on by default; disable with [waggle] enabled = false.
		Waggle:        WaggleConfig{Enabled: true},
		ShowBanner:    true,
		ShowLoader:    true,
		TutorialDone:  false,
		MaxIterations: DefaultMaxIterations,
		// mastermind off by default; when on, the hive spawns this many workers.
		Mastermind:          false,
		MastermindWorkers:   3,
		MastermindReviewers: 1,
		MastermindTriage:    true,
		MastermindParallel:  true,
		Verbose:             false,
		ShowThoughts:        true,
		ShowNudges:          true,
		ShowRecap:           false,
		Compact:             true,
		ShowContextBar:      true,
		Highlight:           true,
		// silent by default: `!ls` runs in your shell without spending tokens
		// on output. Set false (or toggle in /settings) to forward to the LLM.
		ShellBangSilent: true,
		ShowBee:         true,
		ShowContextPct:  true,
		ShowModel:       true,
		ShowCwd:         true,
		ShowEffort:      true,
		ShowTurnTimer:   true,
		ShowGitBranch:   true,
		ShowTotalTokens: true,
		UpdateCheck:     "ask",
		UpdateRepo:      "elhenro/bee",
		UpdateBranch:    "main",
		Providers: map[string]ProviderConfig{
			"openrouter": {
				BaseURL:      "https://openrouter.ai/api/v1",
				WireAPI:      "chat",
				EnvKey:       "OPENROUTER_API_KEY",
				DefaultModel: "deepseek/deepseek-v4-flash",
				// routed aggregator returns real per-call credits in the usage
				// block when asked; opt in so /usage shows actual spend.
				ReportsCost: true,
			},
			"openai": {
				BaseURL:      "https://api.openai.com/v1",
				WireAPI:      "chat",
				EnvKey:       "OPENAI_API_KEY",
				DefaultModel: "gpt-4o-mini",
			},
			"anthropic": {
				BaseURL:      "https://api.anthropic.com/v1",
				WireAPI:      "anthropic-messages",
				EnvKey:       "ANTHROPIC_API_KEY",
				DefaultModel: "claude-sonnet-4-5",
			},
			"gemini": {
				BaseURL:      "https://generativelanguage.googleapis.com/v1beta",
				WireAPI:      "gemini",
				EnvKey:       "GEMINI_API_KEY",
				DefaultModel: "gemini-2.5-flash",
			},
			"ollama": {
				BaseURL:      "http://localhost:11434/v1",
				WireAPI:      "chat",
				EnvKey:       "",
				DefaultModel: "llama3.1:8b",
			},
			// omlx: local MLX inference server for Apple Silicon
			// (github.com/jundot/omlx). OpenAI-compatible at
			// http://localhost:8000/v1. KeyOptional=true: omlx defaults to
			// "skip localhost auth", so an unset key is fine. Set
			// OMLX_API_KEY or run `/login omlx` to enroll a key when omlx
			// was started with `omlx serve --api-key …`.
			"omlx": {
				BaseURL:      "http://localhost:8000/v1",
				WireAPI:      "chat",
				EnvKey:       "OMLX_API_KEY",
				KeyOptional:  true,
				DefaultModel: "qwen2.5-coder-7b",
			},
			// chatgpt: leverage a ChatGPT Plus/Pro/Team subscription via the
			// chatgpt.com responses backend. No API-key billing. Run
			// `/login chatgpt` to drive the PKCE flow.
			//
			// TOS CAVEAT: OpenAI's terms restrict the chatgpt.com backend to
			// their first-party clients. Reusing a public client_id works
			// today but is rate-limited per plan tier and may be revoked.
			// Use at your own risk. The /login chatgpt output surfaces this
			// warning.
			"chatgpt": {
				BaseURL:      "https://chatgpt.com/backend-api/codex",
				WireAPI:      "responses",
				EnvKey:       "",
				DefaultModel: "gpt-5.4-mini",
				OAuth: &OAuthConfig{
					ClientID:          "app_EMoamEEZ73f0CkXaXp7hrann",
					AuthorizeEndpoint: "https://auth.openai.com/oauth/authorize",
					TokenEndpoint:     "https://auth.openai.com/oauth/token",
					// Exact scope/params required by the chatgpt auth server.
					// Hydra rejects deviations: wrong scope or unknown params
					// (e.g. audience) -> authorize_hydra_invalid_request.
					Scope:        "openid profile email offline_access api.connectors.read api.connectors.invoke",
					RedirectPath: "/auth/callback",
					RedirectPort: 1455,
					ExtraAuthorizeParams: map[string]string{
						"id_token_add_organizations": "true",
						"codex_cli_simplified_flow":  "true",
						"originator":                 "codex_cli_rs",
					},
					AccountIDHeader: "chatgpt-account-id",
					AccountIDClaim:  "https://api.openai.com/auth.chatgpt_account_id",
				},
			},
		},
		Profiles: map[string]Profile{
			// tiny: local + 4k-context models (ollama, lmstudio, flash/mini class).
			// Caveman ULTRA: smallest rules block for tightest budget; small
			// models still emit tool_calls. top_k=1 keeps memory injection cheap.
			// Override per-run with `--caveman off` or in config: caveman = "off".
			"tiny": {
				SystemPromptBudget: 3000,
				MemoryTopK:         1,
				MemoryBodyChars:    400,
				ToolDescChars:      220,
				// skills section omitted on tiny (-1 sentinel): 4-tool surface
				// needs no extra advert, every saved byte counts on 4k-context
				// local runs. 0 = unbounded; -1 = drop section entirely.
				SkillManifestChars: -1,
				Caveman:            "ultra",
				MaxIterations:      DefaultMaxIterations,
				// tool format inherits native tool_calls (the global default).
				// capable local models emit clean native calls via the
				// oai-compatible server; the xml textmode wrapper handicaps
				// them and fully breaks models that only speak native FC. opt
				// into "xml" per-run for older models that ignore native call
				// deltas (set tool_format = "xml" in config or a profile).
				// ~1500 tokens (~6k chars) per tool result. one fat read of
				// a 1.5k-line file would otherwise blow a 4-8k MLX context.
				ToolOutputTokens: 1500,
				// search-first discipline for 4k local models: read defaults
				// to 100-line slices (max 500), grep capped at 50 matches.
				// apply_patch dropped — tiny models mis-emit unified diffs.
				SkipApplyPatch:   true,
				ReadDefaultLines: 100,
				ReadMaxLines:     500,
				GrepMaxMatches:   50,
				// nudge tiny after 3 read-only turns: small models loop on reads.
				NoMutationStallThreshold: 3,
				// pin sampling: deterministic for tool turns, prevents temp drift on 4-bit MoE.
				Temperature: 0.0,
				TopP:        0.8,
				// disable post-turn recap on tiny: side-LLM round-trip is ~2s on slow local runs.
				ShowRecap: boolPtr(false),
				// destructive-op approval keys bypass the session AllowSession
				// cache — small models that hallucinate intents can't reuse one
				// "yes" for the rest of the run. mirrors safety.DefaultsForProfile("tiny");
				// kept inline so users can see what they're getting.
				Safety: ProfileSafety{
					RequireApprovalKeys: []string{
						"rm-recursive",
						"find-delete",
						"xargs-rm",
						"find-exec-rm",
						"git-reset-hard",
						"git-push-force",
						"git-push-force-short",
						"git-clean-force",
						"git-branch-delete",
						"chown-root",
						"chmod-world-write",
					},
					WarnOnDuplicateWrites: true,
				},
			},
			// normal: deepseek-flash / gpt-4o-mini class. balanced.
			"normal": {
				SystemPromptBudget: 4000,
				MemoryTopK:         3,
				MemoryBodyChars:    2000,
				ToolDescChars:      160,
				SkillManifestChars: 100,
				Caveman:            "full",
				ToolOutputTokens:   8000,
			},
			// large: sonnet / opus class. headroom for richer prompts.
			"large": {
				SystemPromptBudget: 12000,
				MemoryTopK:         5,
				ToolDescChars:      400,
				SkillManifestChars: 200,
				Caveman:            "lite",
				ToolOutputTokens:   50000,
			},
		},
	}
}
