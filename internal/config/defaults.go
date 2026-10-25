package config

// Defaults returns the canonical out-of-the-box configuration: OpenRouter +
// deepseek-v4-flash, caveman off, danger-full-access +
// on-request sandbox, memory enabled with top_k=3.
//
// Zero-config startup: set OPENROUTER_API_KEY and bee runs.
func Defaults() Config {
	return Config{
		DefaultProvider: "openrouter",
		DefaultModel:    "deepseek/deepseek-v4-flash",
		// off: terse replies hide the rationale. Opt in per-run with `--caveman
		// lite|full|ultra` or in config.toml: caveman = "ultra".
		Caveman: "off",
		// "" = derive from Role per turn (worker→auto, scout→high, queen→max).
		// Override per-run with `--thinking off|low|medium|high|max` (or "auto").
		Thinking: "",
		// "" lets migrateLegacyRoleFields resolve the role on load — from a
		// legacy mode= key if present, else worker. worker is the everyday role:
		// full tool surface with a per-turn 8-token classifier picking read|act,
		// so small models (flash/mini/8b…) don't reflex into shell on greetings.
		Role: "",
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
			Enabled: true,
			TopK:    3,
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
		Waggle: WaggleConfig{Enabled: true},
		// hold new workers while the box is oversubscribed (load per core > 2).
		Swarm:         SwarmConfig{MaxLoadRatio: 2.0},
		ShowBanner:    true,
		ShowLoader:    true,
		MaxIterations: DefaultMaxIterations,
		// system prompt is a ceiling: sections are only trimmed when over it.
		// tool output of 8000 tokens (~32k chars) per call keeps one fat read
		// from flooding the window.
		SystemPromptBudget: 12000,
		MemoryBodyChars:    2000,
		ToolDescChars:      160,
		SkillManifestChars: 100,
		ToolOutputTokens:   8000,
		// mastermind off by default; when on, the hive spawns this many workers.
		MastermindWorkers:   3,
		MastermindReviewers: 1,
		MastermindTriage:    true,
		MastermindParallel:  true,
		ShowThoughts:        true,
		ShowNudges:          true,
		ShowRecap:           true,
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
		ShowLoaderIn:    true,
		ShowLoaderOut:   true,
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
				// OpenRouter honors ephemeral cache_control on the system prefix.
				SupportsPromptCache: true,
				// cheapest upstream first; same as opencode's sort=price.
				RouteSort: "price",
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
				// keep the model resident between turns so the next turn skips a
				// 2-10s reload before first token (ollama unloads after ~5m idle).
				KeepAlive: "15m",
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
	}
}
