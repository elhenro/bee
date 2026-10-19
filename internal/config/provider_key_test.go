package config

import (
	"testing"

	"github.com/elhenro/bee/internal/auth"
)

func TestProviderKeyOrder(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	pc := ProviderConfig{EnvKey: "BEE_TEST_PROVIDER_KEY"}
	t.Setenv("BEE_TEST_PROVIDER_KEY", "")
	if got := ProviderKey("p", pc); got != "" {
		t.Fatalf("no key anywhere: got %q", got)
	}
	dir, _ := auth.DefaultDir()
	if err := auth.SaveAPIKey(dir, "p", "stored"); err != nil {
		t.Fatal(err)
	}
	if got := ProviderKey("p", pc); got != "stored" {
		t.Fatalf("stored key: got %q", got)
	}
	t.Setenv("BEE_TEST_PROVIDER_KEY", "from-env")
	if got := ProviderKey("p", pc); got != "from-env" {
		t.Fatalf("env wins over stored: got %q", got)
	}
	if got := ProviderKey("other", ProviderConfig{}); got != "" {
		t.Fatalf("other provider must not see p's key: got %q", got)
	}
}
