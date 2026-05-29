package web_fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/pelletier/go-toml/v2"
)

// Config holds web_fetch configuration
type Config struct {
	Enabled       bool     `toml:"enabled"`
	AllowDomains  []string `toml:"allow_domains"`
	BlockDomains  []string `toml:"block_domains"`
	MaxContentLen int      `toml:"max_content_len"` // max markdown output length
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		AllowDomains:  nil, // nil = allow all
		BlockDomains:  []string{"localhost", "127.0.0.1", "0.0.0.0"},
		MaxContentLen: 50000,
	}
}

// Tool implements the tools.Tool interface for web fetching
type Tool struct {
	config Config
}

// New creates a new web_fetch tool with the given configuration
func New(cfg Config) (tools.Tool, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("web_fetch tool is disabled")
	}
	
	// Load API key from environment if available
	if os.Getenv("BRAVE_API_KEY") != "" {
		// Could integrate Brave API here in the future
	}
	
	return &Tool{config: cfg}, nil
}

// Spec returns the tool specification
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "web_fetch",
		Description: "Fetch a URL and extract its content as markdown. Skips scripts, styles, and navigation elements. Respects domain allow/block lists.",
		PromptSnippet: "fetch a URL → returns http status, content-type, size, markdown body",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "The URL to fetch content from",
				},
			},
			"required": []string{"url"},
		},
	}
}

// Run executes the web fetch operation
func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	rawURL, ok := input["url"].(string)
	if !ok || rawURL == "" {
		return tools.Result{
			Content: "Error: 'url' parameter is required",
			IsError: true,
		}, nil
	}
	
	// Build domain policy
	policy := &DomainPolicy{
		Allow: t.config.AllowDomains,
		Block: t.config.BlockDomains,
	}
	
	// Fetch the URL
	result, err := FetchURL(rawURL, policy)
	if err != nil {
		return tools.Result{
			Content: fmt.Sprintf("Error fetching URL: %v", err),
			IsError: true,
		}, nil
	}
	
	// Truncate if too long
	if t.config.MaxContentLen > 0 && len(result.Markdown) > t.config.MaxContentLen {
		result.Markdown = result.Markdown[:t.config.MaxContentLen] + "\n\n[…truncated]"
	}
	
	// Format result
	output := fmt.Sprintf("URL: %s\nStatus: %d %s\nContent-Type: %s\nSize: %d bytes\nDuration: %dms\n\n%s",
		result.URL,
		result.Code,
		result.CodeText,
		result.ContentType,
		result.Bytes,
		result.DurationMs,
		result.Markdown,
	)
	
	return tools.Result{
		Content: output,
		IsError: false,
	}, nil
}

// LoadConfigFromEnv loads configuration from environment variables
func LoadConfigFromEnv() Config {
	cfg := DefaultConfig()
	
	// Load from environment
	if val := os.Getenv("BEE_WEB_FETCH_ENABLED"); val == "false" {
		cfg.Enabled = false
	}
	
	if val := os.Getenv("BEE_WEB_FETCH_MAX_CONTENT_LEN"); val != "" {
		fmt.Sscanf(val, "%d", &cfg.MaxContentLen)
	}
	
	// Load domains from environment
	if val := os.Getenv("BEE_WEB_FETCH_ALLOW_DOMAINS"); val != "" {
		cfg.AllowDomains = strings.Split(val, ",")
		for i, d := range cfg.AllowDomains {
			cfg.AllowDomains[i] = strings.TrimSpace(d)
		}
	}
	
	if val := os.Getenv("BEE_WEB_FETCH_BLOCK_DOMAINS"); val != "" {
		cfg.BlockDomains = strings.Split(val, ",")
		for i, d := range cfg.BlockDomains {
			cfg.BlockDomains[i] = strings.TrimSpace(d)
		}
	}
	
	return cfg
}

// LoadConfigFromToml loads configuration from a TOML file
func LoadConfigFromToml(path string) (Config, error) {
	cfg := DefaultConfig()
	
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	
	err = toml.Unmarshal(data, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("failed to parse config: %w", err)
	}
	
	return cfg, nil
}

// SaveConfigToToml saves configuration to a TOML file
func (t *Tool) SaveConfigToToml(path string) error {
	data, err := toml.Marshal(t.config)
	if err != nil {
		return err
	}
	
	return os.WriteFile(path, data, 0644)
}

// MarshalJSON implements json.Marshaler for Config
func (c Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	return json.Marshal(&struct{
		Enabled  bool     `json:"enabled"`
		Allow    []string `json:"allow_domains"`
		Block    []string `json:"block_domains"`
		MaxLen   int      `json:"max_content_len"`
	}{
		Enabled: c.Enabled,
		Allow:   c.AllowDomains,
		Block:   c.BlockDomains,
		MaxLen:  c.MaxContentLen,
	})
}

// UnmarshalJSON implements json.Unmarshaler for Config
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct{
		Enabled  bool     `json:"enabled"`
		Allow    []string `json:"allow_domains"`
		Block    []string `json:"block_domains"`
		MaxLen   int      `json:"max_content_len"`
	}{
		Enabled: true, // default
	}
	
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	
	c.Enabled = aux.Enabled
	c.AllowDomains = aux.Allow
	c.BlockDomains = aux.Block
	c.MaxContentLen = aux.MaxLen
	
	return nil
}