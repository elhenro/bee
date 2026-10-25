package llm

import (
	"cmp"
	"context"
	"fmt"
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
	headers := map[string]string{"Authorization": "Bearer " + tok.AccessToken, "originator": "codex_cli_rs"}
	if accountIDHeader != "" && tok.AccountID != "" {
		headers[accountIDHeader] = tok.AccountID
	}
	var body struct {
		Models []struct {
			Slug          string `json:"slug"`
			DisplayName   string `json:"display_name"`
			ContextWindow int    `json:"context_window"`
			Visibility    string `json:"visibility"`
		} `json:"models"`
	}
	if err := getJSON(ctx, strings.TrimRight(baseURL, "/")+"/models?client_version=9.9.9", headers, &body); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Slug == "" || (m.Visibility != "" && m.Visibility != "list") {
			continue
		}
		out = append(out, Model{ID: m.Slug, Name: cmp.Or(m.DisplayName, m.Slug), ContextLength: m.ContextWindow})
	}
	return out, nil
}
