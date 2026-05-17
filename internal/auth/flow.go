package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Token mirrors the standard OAuth 2.0 token endpoint response.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresIn    int       `json:"expires_in"`
	TokenType    string    `json:"token_type"`
	IssuedAt     time.Time `json:"issued_at"`
	// IDToken is the OIDC id_token, when the provider returns one. Carries
	// per-account claims like ChatGPT's chatgpt_account_id.
	IDToken string `json:"id_token,omitempty"`
	// AccountID is extracted from IDToken at login time and cached so the
	// provider adapter doesn't re-decode the JWT on every request.
	AccountID string `json:"account_id,omitempty"`
}

// expirySkew is the safety margin subtracted from token lifetime before
// declaring it expired. Avoids the in-flight expiry race where a token is
// valid at send time but rejected by the time the request lands.
const expirySkew = 5 * time.Minute

// Expired returns true if the token has passed its expiration time (with a
// 5min safety skew). Tokens with ExpiresIn <= 0 are treated as non-expiring
// (provider didn't tell us).
func (t *Token) Expired() bool {
	if t == nil || t.ExpiresIn <= 0 {
		return false
	}
	deadline := t.IssuedAt.Add(time.Duration(t.ExpiresIn)*time.Second - expirySkew)
	return time.Now().After(deadline)
}

// httpClient is overridable for tests.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// ExchangeCode swaps an authorization code for a token via the token endpoint.
func ExchangeCode(ctx context.Context, tokenURL, clientID, code, verifier, redirectURI string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)
	return postForm(ctx, tokenURL, form)
}

// RefreshToken swaps a refresh token for a new access token.
func RefreshToken(ctx context.Context, tokenURL, clientID, refreshToken string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	return postForm(ctx, tokenURL, form)
}

func postForm(ctx context.Context, tokenURL string, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return doTokenRequest(req)
}

func doTokenRequest(req *http.Request) (*Token, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// Tolerate provider-specific extras (e.g. id_token) by decoding into a
	// raw map first, then projecting the standard fields.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	tk := Token{IssuedAt: time.Now()}
	if v, ok := raw["access_token"].(string); ok {
		tk.AccessToken = v
	}
	if v, ok := raw["refresh_token"].(string); ok {
		tk.RefreshToken = v
	}
	if v, ok := raw["token_type"].(string); ok {
		tk.TokenType = v
	}
	if v, ok := raw["id_token"].(string); ok {
		tk.IDToken = v
	}
	switch v := raw["expires_in"].(type) {
	case float64:
		tk.ExpiresIn = int(v)
	case int:
		tk.ExpiresIn = v
	}
	return &tk, nil
}

// LoginConfig captures everything Login needs to drive the PKCE flow.
type LoginConfig struct {
	ClientID          string
	AuthorizeEndpoint string
	TokenEndpoint     string
	Scope             string
	RedirectPath      string
	Stdout            io.Writer
	// RedirectPort pins the loopback port (0 = random). Some providers
	// require an exact registered redirect_uri.
	RedirectPort int
	// ExtraAuthorizeParams are merged into the authorize URL query string
	// (e.g. {"audience": "..."}).
	ExtraAuthorizeParams map[string]string
	// AccountIDClaim, when set, instructs Login to decode id_token and
	// extract this claim path into Token.AccountID. Dotted path; first
	// segment may be a fully-qualified URI claim (e.g.
	// "https://api.openai.com/auth.chatgpt_account_id").
	AccountIDClaim string
}

// Login runs the full PKCE flow: starts a loopback server, opens the browser,
// waits for the callback, validates state, exchanges the code, and returns
// the resulting Token. Caller persists via SaveToken.
func Login(ctx context.Context, cfg LoginConfig) (*Token, error) {
	if cfg.ClientID == "" || cfg.AuthorizeEndpoint == "" || cfg.TokenEndpoint == "" {
		return nil, fmt.Errorf("missing required oauth config (client_id, authorize_endpoint, token_endpoint)")
	}
	verifier, err := GenerateVerifier()
	if err != nil {
		return nil, err
	}
	challenge := Challenge(verifier)
	state, err := GenerateVerifier()
	if err != nil {
		return nil, err
	}

	srv, err := StartLoopbackOn(ctx, cfg.RedirectPath, cfg.RedirectPort)
	if err != nil {
		return nil, err
	}
	defer srv.Close()

	authURL := BuildAuthorizeURLWithExtras(cfg.AuthorizeEndpoint, cfg.ClientID, srv.URL, challenge, state, cfg.Scope, cfg.ExtraAuthorizeParams)
	if oerr := openURL(authURL); oerr != nil && cfg.Stdout != nil {
		fmt.Fprintf(cfg.Stdout, "open this URL in your browser:\n%s\n", authURL)
	} else if cfg.Stdout != nil {
		fmt.Fprintf(cfg.Stdout, "opening browser for OAuth login... if it doesn't open:\n%s\n", authURL)
	}

	select {
	case res := <-srv.Result:
		if res.Err != nil {
			return nil, res.Err
		}
		if res.State != state {
			return nil, fmt.Errorf("oauth state mismatch (possible CSRF)")
		}
		tok, err := ExchangeCode(ctx, cfg.TokenEndpoint, cfg.ClientID, res.Code, verifier, srv.URL)
		if err != nil {
			return nil, err
		}
		if cfg.AccountIDClaim != "" && tok.IDToken != "" {
			tok.AccountID = ExtractClaim(tok.IDToken, cfg.AccountIDClaim)
		}
		return tok, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
