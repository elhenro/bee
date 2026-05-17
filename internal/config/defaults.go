package config

// Defaults returns the canonical out-of-the-box configuration: OpenRouter +
// deepseek-v4-flash, three profiles, caveman-full, workspace-write +
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
		// "auto" = medium when model supports reasoning_effort/thinking-budget
		// (o-series, gpt-5, claude-4.x, gemini-2.5, deepseek reasoner, qwq …),
		// off otherwise. Override per-run with `--thinking off|low|medium|high`.
		Thinking: "auto",
		// "auto" runs an 8-token classifier per turn to pick plan|edit. Cheap
		// and avoids mutator spam on greetings/questions where small models
		// (flash/mini/8b…) otherwise reflex into shell calls.
		Mode: "auto",
		Sandbox: SandboxConfig{
			Scope:    "workspace-write",
			Approval: "on-request",
		},
		Memory: MemoryConfig{
			Enabled:             true,
			TopK:                3,
			BackgroundExtractor: false,
		},
		Compaction: CompactionConfig{
			Enabled:   true,
			Threshold: 0.8,
		},
		ShowBanner:    true,
		MaxIterations: 50,
		Verbose:        false,
		ShowThoughts:   true,
		ShowNudges:     false,
		Compact:        false,
		ShowContextBar: false,
		Highlight:      true,
		Providers: map[string]ProviderConfig{
			"openrouter": {
				BaseURL:      "https://openrouter.ai/api/v1",
				WireAPI:      "chat",
				EnvKey:       "OPENROUTER_API_KEY",
				DefaultModel: "deepseek/deepseek-v4-flash",
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
				DefaultModel: "gemini-2.0-flash",
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
				DefaultModel: "gpt-5-codex",
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
			// Caveman FULL: small models tolerate the terse style and still emit
			// tool_calls in practice. top_k=1 keeps memory injection cheap.
			// Override per-run with `--caveman off` or in config: caveman = "off".
			"tiny": {
				SystemPromptBudget: 3000,
				MemoryTopK:         1,
				MemoryBodyChars:    400,
				ToolDescChars:      220,
				SkillManifestChars: 80,
				Caveman:            "full",
				MaxIterations:      50,
				// ~1500 tokens (~6k chars) per tool result. one fat read of
				// a 1.5k-line file would otherwise blow a 4-8k MLX context.
				ToolOutputTokens: 1500,
				// search-first discipline for 4k local models: read defaults
				// to 100-line slices (max 500), grep capped at 50 matches.
				// apply_patch dropped — tiny models mis-emit unified diffs.
				SkipApplyPatch:           true,
				ReadDefaultLines:         100,
				ReadMaxLines:             500,
				GrepMaxMatches:           50,
				NoMutationStallThreshold: 0,
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
