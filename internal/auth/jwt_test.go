package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	p, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(p)
	return strings.Join([]string{header, payload, ""}, ".")
}

func TestExtractClaim_TopLevel(t *testing.T) {
	jwt := makeJWT(t, map[string]any{"sub": "user-123", "email": "a@b.c"})
	if got := ExtractClaim(jwt, "sub"); got != "user-123" {
		t.Errorf("got %q want user-123", got)
	}
}

func TestExtractClaim_URIClaimNested(t *testing.T) {
	jwt := makeJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-abc",
		},
	})
	got := ExtractClaim(jwt, "https://api.openai.com/auth.chatgpt_account_id")
	if got != "acct-abc" {
		t.Errorf("got %q want acct-abc", got)
	}
}

func TestExtractClaim_Missing(t *testing.T) {
	jwt := makeJWT(t, map[string]any{"sub": "x"})
	if got := ExtractClaim(jwt, "nope"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractClaim_MalformedJWT(t *testing.T) {
	if got := ExtractClaim("not.a.jwt!!", "sub"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	if got := ExtractClaim("onlyone", "sub"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
