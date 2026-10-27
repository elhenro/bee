package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/flock"
)

// PersistPick rewrites ~/.bee/config.toml so the next launch defaults to the
// chosen provider+model.
//
// Trade-off: we do NOT recreate the live Engine mid-session. The choice is
// durable, but takes effect on the next bee run. Live swap would require
// draining tool-call state cleanly in loop/turn.go.
func PersistPick(path string, provider, model string) error {
	if provider == "" {
		return fmt.Errorf("persist pick: empty provider")
	}
	set := map[string]any{"default_provider": provider}
	if model != "" {
		set["default_model"] = model
	}
	return persistKeys(path, set)
}

// PersistSetting rewrites ~/.bee/config.toml setting top-level key=value while
// preserving every other field. Used by /settings to make verbosity / thought
// visibility toggles survive across launches. key must be a top-level TOML
// key (no dotted paths). value must be TOML-encodable.
func PersistSetting(path string, key string, value any) error {
	if key == "" {
		return fmt.Errorf("persist setting: empty key")
	}
	return persistKeys(path, map[string]any{key: value})
}

// persistKeys sets top-level keys in the config at path (default: the user
// config). The read-modify-write runs under a cross-process lock so two bees
// toggling settings don't lose each other's write.
func persistKeys(path string, set map[string]any) error {
	if path == "" {
		path = config.ConfigPath()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	defer flock.Lock(path)()

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	out, err := patchTOML(data, set)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := atomicfile.Write(path, out, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
