package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FetchChatGPTModels GETs /backend-api/codex/models?client_version=. The
// server returns a plan-filtered model list once authed; without a high enough
// client_version it only surfaces a single legacy entry. We send 9.9.9 to opt
// into the full set the account is entitled to.
func FetchChatGPTModels(ctx context.Context, baseURL, accountIDHeader string) ([]Model, error) {
	tok := loadAuthToken("chatgpt")
	if tok == nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("chatgpt: not logged in (run /login chatgpt)")
	}
	url := strings.TrimRight(baseURL, "/") + "/models?client_version=9.9.9"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("originator", "codex_cli_rs")
	if accountIDHeader != "" && tok.AccountID != "" {
		req.Header.Set(accountIDHeader, tok.AccountID)
	}
	resp, err := modelsHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}
	var body struct {
		Models []struct {
			Slug          string `json:"slug"`
			DisplayName   string `json:"display_name"`
			ContextWindow int    `json:"context_window"`
			Visibility    string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]Model, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Slug == "" || (m.Visibility != "" && m.Visibility != "list") {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		out = append(out, Model{ID: m.Slug, Name: name, ContextLength: m.ContextWindow})
	}
	return out, nil
}
