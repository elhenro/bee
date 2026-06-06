package config

// VisionConfig points the loop's vision fallback at a multimodal model. Model
// empty -> no fallback: images hitting a non-vision main model are dropped with
// a one-time hint to configure this or run /vision. API selects the wire shape:
// "openai" (default — omlx/LM Studio/vLLM/hosted qwen-VL) or "ollama".
type VisionConfig struct {
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint"` // base url, e.g. http://localhost:8080/v1
	API      string `toml:"api"`      // "openai" (default) | "ollama"
	EnvKey   string `toml:"env_key"`  // env var holding the api key (optional for local)
	// Provider, when set, inherits base_url + env_key from that configured
	// [providers.<name>] entry, so a vision model served by the same backend
	// as the main model needs only `provider` + `model`. Explicit Endpoint /
	// EnvKey here still win.
	Provider string `toml:"provider"`
}
