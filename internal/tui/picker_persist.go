package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/elhenro/bee/internal/config"
)

// PersistPick rewrites ~/.bee/config.toml so the next launch defaults to the
// chosen provider+model.
//
// Trade-off: we do NOT recreate the live Engine mid-session. The choice is
// durable, but takes effect on the next bee run. Live swap would require
// draining tool-call state cleanly in loop/turn.go.
func PersistPick(path string, provider, model string) error {
	if path == "" {
		path = config.ConfigPath()
	}
	if provider == "" {
		return fmt.Errorf("persist pick: empty provider")
	}

	root, err := readTOMLTree(path)
	if err != nil {
		return err
	}
	root["default_provider"] = provider
	if model != "" {
		root["default_model"] = model
	}

	return atomicWriteTOML(path, root)
}

// PersistSetting rewrites ~/.bee/config.toml setting top-level key=value while
// preserving every other field. Used by /settings to make verbosity / thought
// visibility toggles survive across launches. key must be a top-level TOML
// key (no dotted paths). value must be a TOML-encodable scalar.
func PersistSetting(path string, key string, value any) error {
	if path == "" {
		path = config.ConfigPath()
	}
	if key == "" {
		return fmt.Errorf("persist setting: empty key")
	}
	root, err := readTOMLTree(path)
	if err != nil {
		return err
	}
	root[key] = value
	return atomicWriteTOML(path, root)
}

// readTOMLTree loads the TOML at path into a generic map. Missing file yields
// an empty map — we still want to write a fresh config in that case.
func readTOMLTree(path string) (map[string]any, error) {
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return root, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return root, nil
	}
	if err := toml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return root, nil
}

// atomicWriteTOML serializes root, writes to a sibling tmp file, then renames.
// Same-directory rename is atomic on POSIX; on Windows it's near-atomic.
func atomicWriteTOML(path string, root map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	out, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal toml: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.toml.*")
	if err != nil {
		return fmt.Errorf("tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// best-effort cleanup if rename failed
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
