package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/elhenro/bee/internal/auth"
)

// Load resolves the merged config: Defaults < ~/.bee/config.toml < env vars.
// Finally, looks up the active provider's EnvKey to populate APIKey; returns
// a clear error if the key is required but missing.
func Load() (Config, error) {
	c := Defaults()

	if err := mergeFile(&c, ConfigPath()); err != nil {
		return c, err
	}
	mergeEnv(&c)
	c = ApplyProfile(c)

	if err := resolveAPIKey(&c); err != nil {
		return c, err
	}
	return c, nil
}

// mergeFile decodes the TOML at path (if it exists) into c. A missing file
// is not an error — defaults stand. Decoding errors are surfaced.
func mergeFile(c *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	// Decode onto the existing struct so unset TOML keys keep their default
	// values. go-toml merges per-key: a partial [profiles.tiny] in the file
	// overrides only the fields it names, leaving tiny's other fields and the
	// other profiles intact. This is what lets a bench overlay flip one knob.
	if err := toml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// mergeEnv overrides selected top-level fields from environment variables.
// Empty env vars are ignored.
func mergeEnv(c *Config) {
	if v := os.Getenv("BEE_PROVIDER"); v != "" {
		c.DefaultProvider = v
	}
	if v := os.Getenv("BEE_MODEL"); v != "" {
		c.DefaultModel = v
	}
	if v := os.Getenv("BEE_CAVEMAN"); v != "" {
		c.Caveman = v
	}
	if v := os.Getenv("BEE_PROFILE"); v != "" {
		c.Profile = v
	}
	if v := os.Getenv("BEE_MODE"); v != "" {
		c.Mode = v
	}
	if v := os.Getenv("BEE_EXTRA_TOOLS"); v != "" {
		c.ExtraTools = splitCSV(v)
	}
	if v := os.Getenv("BEE_SHELL_RC"); v != "" {
		c.Shell.UseUserRC = boolEnv(v)
	}
	if v := os.Getenv("BEE_SHELL"); v != "" {
		c.Shell.Shell = v
	}
	if v := os.Getenv("BEE_SHELL_RC_FILE"); v != "" {
		c.Shell.RCFile = v
	}
}

// boolEnv parses common truthy spellings. Anything else = false.
func boolEnv(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// splitCSV trims whitespace, drops empties.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// resolveAPIKey looks up the active provider's auth credential. Resolution
// order: env var (prov.EnvKey) → ~/.bee/auth/<provider>.key (set via
// /login <provider>) → error (unless prov.KeyOptional, e.g. omlx running
// localhost without --api-key).
//
// An empty EnvKey (e.g. local ollama) skips the lookup entirely.
func resolveAPIKey(c *Config) error {
	prov, ok := c.Providers[c.DefaultProvider]
	if !ok {
		return fmt.Errorf("config: provider %q not defined in [providers]", c.DefaultProvider)
	}
	if prov.EnvKey == "" {
		return nil
	}
	if key := os.Getenv(prov.EnvKey); key != "" {
		c.APIKey = key
		return nil
	}
	if dir, err := auth.DefaultDir(); err == nil {
		if key, err := auth.LoadAPIKey(dir, c.DefaultProvider); err == nil && key != "" {
			c.APIKey = key
			return nil
		}
	}
	if prov.KeyOptional {
		return nil
	}
	return fmt.Errorf("config: provider %q requires %s in environment (or run `/login %s` to store one)",
		c.DefaultProvider, prov.EnvKey, c.DefaultProvider)
}
