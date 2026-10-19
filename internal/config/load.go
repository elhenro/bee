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
	return LoadWith(Overrides{})
}

// Overrides are per-invocation choices (CLI flags) that must apply before the
// profile and API key resolve, so a --provider flag picks that provider's key,
// auto profile and model rather than the configured default's.
type Overrides struct {
	Provider string
	Model    string
}

// LoadWith is Load with flag overrides folded in ahead of profile/key
// resolution. A provider override without a model takes that provider's
// default_model, since the configured model id belongs to the old provider.
func LoadWith(o Overrides) (Config, error) {
	c := Defaults()
	baseModel := c.DefaultModel

	if err := mergeFile(&c, ConfigPath()); err != nil {
		return c, err
	}
	explicitModel := c.DefaultModel != baseModel
	mergeEnv(&c)
	explicitModel = explicitModel || os.Getenv("BEE_MODEL") != ""
	if !explicitModel {
		useProviderModel(&c)
	}
	if o.Provider != "" && o.Provider != c.DefaultProvider {
		c.DefaultProvider = o.Provider
		useProviderModel(&c)
	}
	if o.Model != "" {
		c.DefaultModel = o.Model
	}
	migrateLegacyRoleFields(&c)
	c = ApplyProfile(c)

	if err := resolveAPIKey(&c); err != nil {
		return c, err
	}
	return c, nil
}

// useProviderModel sets the model to the active provider's default_model when
// that provider declares one.
func useProviderModel(c *Config) {
	if m := c.Providers[c.DefaultProvider].DefaultModel; m != "" {
		c.DefaultModel = m
	}
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
	if v := os.Getenv("BEE_ROLE"); v != "" {
		c.Role = v
	}
	if v := os.Getenv("BEE_YOLO"); v != "" {
		c.Yolo = boolEnv(v)
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

// migrateLegacyRoleFields resolves Role from the deprecated mode/mastermind
// keys when a config (or env) hasn't set the new role axis. An explicit Role
// (file or BEE_ROLE) is respected as-is. Mapping: plan→scout, yolo→worker +
// Yolo toggle, auto/edit/unknown→worker; a legacy mastermind=true upgrades the
// result to queen (the hive flag wins over the mode-derived role).
func migrateLegacyRoleFields(c *Config) {
	if c.Role != "" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "plan":
		c.Role = "scout"
	case "yolo":
		c.Role = "worker"
		c.Yolo = true
	default:
		c.Role = "worker"
	}
	if c.Mastermind {
		c.Role = "queen"
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

// ProviderKey resolves the credential for one provider block: env var
// (pc.EnvKey) first, then the key stored by /login under name. It never
// reflects another provider, so callers stay correct after a mid-session
// provider switch (Config.APIKey only describes the provider chosen at load).
func ProviderKey(name string, pc ProviderConfig) string {
	if pc.EnvKey != "" {
		if key := os.Getenv(pc.EnvKey); key != "" {
			return key
		}
	}
	if dir, err := auth.DefaultDir(); err == nil {
		if key, err := auth.LoadAPIKey(dir, name); err == nil && key != "" {
			return key
		}
	}
	return ""
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
	if key := ProviderKey(c.DefaultProvider, prov); key != "" {
		c.APIKey = key
		return nil
	}
	if prov.KeyOptional {
		return nil
	}
	return fmt.Errorf("config: provider %q requires %s in environment (or run `/login %s` to store one)",
		c.DefaultProvider, prov.EnvKey, c.DefaultProvider)
}
