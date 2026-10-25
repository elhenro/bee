package config

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
	// ChatTemplateKwargs flows into the MLX/vllm-extended chat completion body
	// as `chat_template_kwargs`. Used to flip Qwen3 / Hermes chat-template
	// switches like `enable_thinking=false` so the model emits canonical tool
	// envelopes instead of prose summaries. Omitted when empty.
	ChatTemplateKwargs map[string]any `toml:"chat_template_kwargs"`
	// ReportsCost opts this provider into a request flag asking the service to
	// return actual per-call spend in the usage block. Set only for routed
	// aggregators that return real cost; strict endpoints reject the field.
	ReportsCost bool `toml:"reports_cost"`
	// KeepAlive sets Ollama's `keep_alive` (e.g. "15m") so the model stays
	// resident between turns instead of unloading after the idle timeout. Empty
	// = omit; only meaningful for local providers that unload (Ollama).
	KeepAlive string `toml:"keep_alive"`
	// SupportsPromptCache opts the provider into ephemeral cache_control
	// breakpoints on the system prefix. On by default for OpenRouter; off for
	// strict endpoints that reject the content-parts system message.
	SupportsPromptCache bool `toml:"supports_prompt_cache"`
	// RouteSort orders a routed aggregator's upstreams by "price" (cheapest
	// first), "throughput" or "latency". On by default (price) for OpenRouter;
	// "none" or empty omits it. Strict endpoints reject the field.
	RouteSort string `toml:"route_sort"`
	// NumCtx sets Ollama's runtime context window. >0 is sent verbatim (operator
	// override). 0 lets bee auto-allocate the probed window (clamped) for
	// ollama-shaped servers. Only meaningful for local providers.
	NumCtx int `toml:"num_ctx"`
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
