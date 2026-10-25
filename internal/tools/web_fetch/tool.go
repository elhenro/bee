package web_fetch

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// Config holds web_fetch configuration
type Config struct {
	Enabled       bool     `toml:"enabled"`
	AllowDomains  []string `toml:"allow_domains"`
	BlockDomains  []string `toml:"block_domains"`
	MaxContentLen int      `toml:"max_content_len"` // max markdown output length
	// Confined gates the SSRF guard (block loopback/private/metadata IPs). Set
	// true under a confined sandbox scope; false under danger-full-access, where
	// the model may fetch localhost/LAN just as it could via shell curl. Derived
	// from the scope, not user config, so it isn't serialized.
	Confined bool `toml:"-" json:"-"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		Enabled: true,
		// nil allow/block lists = allow all. SSRF (loopback/private/metadata) is
		// handled by the scope-gated isBlockedIP guard in FetchURL, which covers
		// more than block literals would; listing them here would block localhost
		// even in danger mode.
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
	return &Tool{config: cfg}, nil
}

// Spec returns the tool specification
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          "web_fetch",
		Description:   "Fetch a URL and extract its content as markdown. Skips scripts, styles, and navigation elements. Respects domain allow/block lists.",
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
		return tools.Result{Content: "Error: 'url' parameter is required", IsError: true}, nil
	}

	// Build domain policy
	policy := &DomainPolicy{Allow: t.config.AllowDomains, Block: t.config.BlockDomains}

	// Fetch the URL. Confined drives the SSRF guard: on under a confined scope,
	// off under danger-full-access (localhost/LAN reachable, same as shell curl).
	result, err := FetchURL(rawURL, policy, t.config.Confined)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Error fetching URL: %v", err), IsError: true}, nil
	}

	// Truncate if too long, backing off to a rune boundary so the cut never
	// splits a UTF-8 sequence.
	if t.config.MaxContentLen > 0 && len(result.Markdown) > t.config.MaxContentLen {
		cut := t.config.MaxContentLen
		for cut > 0 && !utf8.RuneStart(result.Markdown[cut]) {
			cut--
		}
		result.Markdown = result.Markdown[:cut] + "\n\n[…truncated]"
	}

	// Format result
	output := fmt.Sprintf("URL: %s\nStatus: %d %s\nContent-Type: %s\nSize: %d bytes\nDuration: %dms\n\n%s",
		result.URL, result.Code, result.CodeText, result.ContentType, result.Bytes, result.DurationMs, result.Markdown)
	return tools.Result{Content: output}, nil
}
