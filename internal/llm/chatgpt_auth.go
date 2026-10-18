package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/elhenro/bee/internal/auth"
)

// refresh swaps the stored refresh_token for a fresh access_token. Returns
// (true, nil) on success. Concurrent callers serialize on p.mu (in process) and
// a file lock (across processes); stale is the access token the failed request
// carried, so a caller that lost the race sees the rotated token on disk and
// retries with it instead of spending the already-rotated refresh_token.
func (p *ChatGPTProvider) refresh(ctx context.Context, stale string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(home, ".bee", "auth")
	defer auth.LockToken(dir, p.cfg.Name)()
	tok, err := auth.LoadToken(dir, p.cfg.Name)
	if err != nil || tok == nil {
		return false, fmt.Errorf("no token to refresh")
	}
	if stale != "" && tok.AccessToken != "" && tok.AccessToken != stale {
		return true, nil
	}
	if tok.RefreshToken == "" {
		return false, fmt.Errorf("token has no refresh_token")
	}
	newTok, err := auth.RefreshToken(ctx, p.cfg.TokenEndpoint, p.cfg.ClientID, tok.RefreshToken)
	if err != nil {
		return false, err
	}
	// Preserve fields the refresh response usually omits.
	if newTok.RefreshToken == "" {
		newTok.RefreshToken = tok.RefreshToken
	}
	if newTok.IDToken == "" {
		newTok.IDToken = tok.IDToken
	}
	if newTok.AccountID == "" {
		newTok.AccountID = tok.AccountID
	}
	if err := auth.SaveToken(dir, p.cfg.Name, newTok); err != nil {
		return false, err
	}
	return true, nil
}

// loadAuthToken is the full-token variant of loadAuthBearer in openai_compat.go.
func loadAuthToken(provider string) *auth.Token {
	if provider == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	tok, err := auth.LoadToken(filepath.Join(home, ".bee", "auth"), provider)
	if err != nil {
		return nil
	}
	return tok
}
