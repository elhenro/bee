package llm

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/elhenro/bee/internal/auth"
)

func (p *OpenAICompatProvider) applyHeaders(r *http.Request, streaming bool) {
	r.Header.Set("Content-Type", "application/json")
	if streaming {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
	r.Header.Set("HTTP-Referer", "https://github.com/elhenro/bee")
	r.Header.Set("X-Title", "bee")
	// Auth resolution order:
	//   1. OAuth bearer token saved by /login
	//   2. static api key saved by /login (apikey.SaveAPIKey)
	//   3. environment variable named by EnvKey
	// (1) and (2) live in ~/.bee/auth/ as <provider>.json and <provider>.key
	// respectively. Without (2) the static api key path is dead storage and
	// the user sees 401 even after "✓ key saved".
	if tok := loadAuthBearer(p.cfg.Name); tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	} else if key := loadAuthAPIKey(p.cfg.Name); key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	} else if p.cfg.EnvKey != "" {
		if key := os.Getenv(p.cfg.EnvKey); key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
	}
	for k, v := range p.cfg.ExtraHeaders {
		r.Header.Set(k, v)
	}
}

// loadAuthBearer returns the access_token saved by /login for provider, or
// "" if no token file exists or it's unreadable. Expired tokens are still
// returned (the API will respond 401 and the user re-runs /login).
func loadAuthBearer(provider string) string {
	if provider == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	tok, err := auth.LoadToken(filepath.Join(home, ".bee", "auth"), provider)
	if err != nil || tok == nil {
		return ""
	}
	return tok.AccessToken
}

// loadAuthAPIKey returns the static api key saved by /login (the api-key
// sub-mode in LoginPane) for provider, or "" if absent. Mirrors
// loadAuthBearer's lookup so static-key providers (omlx, deepseek, groq, …)
// pick up the same ~/.bee/auth/<provider>.key file the login pane writes.
func loadAuthAPIKey(provider string) string {
	if provider == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	key, err := auth.LoadAPIKey(filepath.Join(home, ".bee", "auth"), provider)
	if err != nil {
		return ""
	}
	return key
}
