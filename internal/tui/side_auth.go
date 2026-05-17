package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/commands"
)

// Login runs the OAuth PKCE flow for the named provider. Provider must have
// an [oauth] block in config; otherwise this errors out. Token is persisted
// under ~/.bee/auth/<provider>.json with 0600 perms.
func (s *tuiSide) Login(ctx context.Context, provider string) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("login: no engine")
	}
	pcfg, ok := s.m.eng.Cfg.Providers[provider]
	if !ok {
		return fmt.Errorf("unknown provider %q", provider)
	}
	if pcfg.OAuth == nil {
		return fmt.Errorf("provider %q has no [oauth] config in ~/.bee/config.toml", provider)
	}
	dir, err := auth.DefaultDir()
	if err != nil {
		return err
	}
	if provider == "chatgpt" {
		fmt.Fprintln(os.Stderr, "note: /login chatgpt uses a public OpenAI first-party client_id against chatgpt.com.")
		fmt.Fprintln(os.Stderr, "      sanctioned for ChatGPT Plus/Pro/Team accounts; rate-limited per plan; may be revoked.")
	}
	tok, err := auth.Login(ctx, auth.LoginConfig{
		ClientID:             pcfg.OAuth.ClientID,
		AuthorizeEndpoint:    pcfg.OAuth.AuthorizeEndpoint,
		TokenEndpoint:        pcfg.OAuth.TokenEndpoint,
		Scope:                pcfg.OAuth.Scope,
		RedirectPath:         pcfg.OAuth.RedirectPath,
		RedirectPort:         pcfg.OAuth.RedirectPort,
		ExtraAuthorizeParams: pcfg.OAuth.ExtraAuthorizeParams,
		AccountIDClaim:       pcfg.OAuth.AccountIDClaim,
		Stdout:               os.Stderr,
	})
	if err != nil {
		return err
	}
	return auth.SaveToken(dir, provider, tok)
}

// LoginStatus enumerates configured providers and their auth posture
// (oauth configured, token saved, env key set, key file saved). Sorted
// alphabetically; the default provider keeps its position but is flagged
// IsDefault.
func (s *tuiSide) LoginStatus() []commands.ProviderAuth {
	if s.m == nil || s.m.eng == nil {
		return nil
	}
	cfg := s.m.eng.Cfg
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	dir, _ := auth.DefaultDir()
	out := make([]commands.ProviderAuth, 0, len(names))
	for _, n := range names {
		p := cfg.Providers[n]
		entry := commands.ProviderAuth{
			Name:        n,
			HasOAuth:    p.OAuth != nil,
			EnvKey:      p.EnvKey,
			KeyOptional: p.KeyOptional,
			IsDefault:   n == cfg.DefaultProvider,
		}
		if p.EnvKey != "" {
			_, entry.EnvSet = os.LookupEnv(p.EnvKey)
		}
		if dir != "" {
			if tok, err := auth.LoadToken(dir, n); err == nil && tok != nil {
				entry.TokenSaved = true
			}
			entry.KeySaved = auth.HasAPIKey(dir, n)
		}
		out = append(out, entry)
	}
	return out
}

// OpenLogin flips a sentinel that Model.Update consumes to display the
// interactive login pane. Used by the no-arg /login slash command.
func (s *tuiSide) OpenLogin() error {
	if s.m == nil {
		return errors.New("no tui")
	}
	s.m.loginRequested = true
	return nil
}

// Logout removes both the stored OAuth token AND any stored api key file
// for the named provider. Either may be absent — both deletes are no-ops
// on ErrNotExist.
func (s *tuiSide) Logout(provider string) error {
	dir, err := auth.DefaultDir()
	if err != nil {
		return err
	}
	if err := auth.DeleteToken(dir, provider); err != nil {
		return err
	}
	return auth.DeleteAPIKey(dir, provider)
}

// SaveAPIKey persists a static api key for a non-oauth provider. Live
// engine config is updated too so the new key takes effect mid-session
// without a restart (when the saved provider matches the active one).
func (s *tuiSide) SaveAPIKey(provider, key string) error {
	if provider == "" {
		return errors.New("save key: empty provider")
	}
	dir, err := auth.DefaultDir()
	if err != nil {
		return err
	}
	if err := auth.SaveAPIKey(dir, provider, key); err != nil {
		return err
	}
	if s.m != nil && s.m.eng != nil && s.m.eng.Cfg.DefaultProvider == provider {
		s.m.eng.Cfg.APIKey = key
	}
	return nil
}
