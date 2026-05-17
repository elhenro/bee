package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveLoadDelete_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	tok := &Token{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresIn:    3600,
		TokenType:    "Bearer",
		IssuedAt:     time.Now().Truncate(time.Second),
	}
	if err := SaveToken(dir, "demo", tok); err != nil {
		t.Fatal(err)
	}
	got, err := LoadToken(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected token, got nil")
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" || got.ExpiresIn != 3600 {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if err := DeleteToken(dir, "demo"); err != nil {
		t.Fatal(err)
	}
	got2, err := LoadToken(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got2 != nil {
		t.Errorf("expected nil after delete, got %+v", got2)
	}
}

func TestLoadToken_Missing(t *testing.T) {
	dir := t.TempDir()
	tok, err := LoadToken(dir, "nope")
	if err != nil {
		t.Fatal(err)
	}
	if tok != nil {
		t.Errorf("expected nil for missing, got %+v", tok)
	}
}

func TestSaveToken_FilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm bits don't apply on windows")
	}
	dir := t.TempDir()
	tok := &Token{AccessToken: "x"}
	if err := SaveToken(dir, "p", tok); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600, got %o", st.Mode().Perm())
	}
}

func TestDeleteToken_Missing_NoError(t *testing.T) {
	dir := t.TempDir()
	if err := DeleteToken(dir, "nope"); err != nil {
		t.Errorf("delete of missing should be no-op: %v", err)
	}
}

func TestSaveToken_NilErrors(t *testing.T) {
	dir := t.TempDir()
	if err := SaveToken(dir, "p", nil); err == nil {
		t.Error("expected error on nil token")
	}
}
