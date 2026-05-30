package config

// BrowserConfig gates and configures the native browser tools. Off by
// default to keep the tool surface lean for tiny-context profiles; flip via
// config or the --browser flag.
type BrowserConfig struct {
	Enabled    bool                `toml:"enabled"`
	Headless   bool                `toml:"headless"`    // default false: headful so the user can watch
	ChromePath string              `toml:"chrome_path"` // empty -> auto-detect
	Vision     BrowserVisionConfig `toml:"vision"`
}

// BrowserVisionConfig points browser_screenshot at a local vision model.
// Model empty -> the screenshot tool is not registered.
type BrowserVisionConfig struct {
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint"` // ollama base url, e.g. http://localhost:11434
}
