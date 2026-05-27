package web_search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// Config holds web_search configuration.
type Config struct {
	Enabled bool `toml:"enabled"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled: true,
	}
}

// Tool implements the tools.Tool interface for web search via Brave API.
type Tool struct {
	config Config
}

// New creates a new web_search tool with the given configuration.
func New(cfg Config) (tools.Tool, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("web_search tool is disabled")
	}
	return &Tool{config: cfg}, nil
}

// Spec returns the tool specification.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "web_search",
		Description: "Search the web via Brave Search API. Returns top 5 results with title, url, snippet.",
		PromptSnippet: "search web for QUERY → returns top 5 {title, url, snippet}",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query string.",
				},
				"count": map[string]any{
					"type":        "integer",
					"description": "Number of results to return (1-20, default 5).",
				},
			},
			"required": []string{"query"},
		},
	}
}

// Run executes the web search operation.
func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	query, ok := input["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return tools.Result{
			Content: "Error: 'query' parameter is required",
			IsError: true,
		}, nil
	}

	// Check for Brave API key
	apiKey := os.Getenv("BRAVE_API_KEY")
	if apiKey == "" {
		return tools.Result{
			Content: "web_search disabled: BRAVE_API_KEY not set in environment",
			IsError: true,
		}, nil
	}

	// Parse count parameter
	count := 5
	if c, ok := input["count"].(float64); ok {
		if c >= 1 && c <= 20 {
			count = int(c)
		}
	}

	// Build request URL
	encodedQuery := url.QueryEscape(query)
	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d", encodedQuery, count)

	// Create HTTP client with 15s timeout
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return tools.Result{
			Content: fmt.Sprintf("Error building request: %v", err),
			IsError: true,
		}, nil
	}

	// Set headers
	req.Header.Set("X-Subscription-Token", apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bee/0.1 (+https://github.com/elhenro/bee)")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return tools.Result{
			Content: fmt.Sprintf("Error making request: %v", err),
			IsError: true,
		}, nil
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.Result{
			Content: fmt.Sprintf("Error reading response: %v", err),
			IsError: true,
		}, nil
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		return tools.Result{
			Content: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, bodyStr),
			IsError: true,
		}, nil
	}

	// Parse JSON response
	var searchResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}

	if err := json.Unmarshal(body, &searchResp); err != nil {
		return tools.Result{
			Content: fmt.Sprintf("Error parsing JSON response: %v", err),
			IsError: true,
		}, nil
	}

	// Format results as numbered list
	var sb strings.Builder
	for i, r := range searchResp.Web.Results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description))
	}

	return tools.Result{
		Content: sb.String(),
	}, nil
}