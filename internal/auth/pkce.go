// Package auth implements OAuth 2.0 PKCE flow scaffolding for bee.
//
// Generic by design: no provider is hard-coded. Users add OAuth client_id +
// endpoints under [providers.<name>.oauth] in ~/.bee/config.toml, then run
// /login <name> to drive the flow and persist tokens under ~/.bee/auth/.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// GenerateVerifier returns a 43-128 char URL-safe code_verifier per RFC 7636.
// Uses 64 random bytes -> 86 chars after base64url-no-pad. Cryptographically strong.
func GenerateVerifier() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Challenge returns base64url(sha256(verifier)) — the S256 PKCE challenge.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
