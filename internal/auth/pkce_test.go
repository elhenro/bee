package auth

import (
	"regexp"
	"testing"
)

func TestGenerateVerifier_LengthAndCharset(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Errorf("verifier length out of RFC 7636 range: %d", len(v))
	}
	// base64url-no-pad: A-Z, a-z, 0-9, '-', '_'
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(v) {
		t.Errorf("verifier has invalid chars: %q", v)
	}
}

func TestGenerateVerifier_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		v, err := GenerateVerifier()
		if err != nil {
			t.Fatal(err)
		}
		if seen[v] {
			t.Fatalf("collision: %s", v)
		}
		seen[v] = true
	}
}

func TestChallenge_Deterministic(t *testing.T) {
	v := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	c1 := Challenge(v)
	c2 := Challenge(v)
	if c1 != c2 {
		t.Errorf("non-deterministic: %s vs %s", c1, c2)
	}
	// rfc 7636 appendix B fixture
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if c1 != want {
		t.Errorf("Challenge: got %s want %s", c1, want)
	}
}

func TestChallenge_DifferentVerifiers(t *testing.T) {
	if Challenge("a") == Challenge("b") {
		t.Error("collision on different inputs")
	}
}
