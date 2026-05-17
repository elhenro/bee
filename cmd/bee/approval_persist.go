package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/elhenro/bee/internal/config"
)

// PersistAllowlistEntry appends key to sandbox.command_allowlist in the user's
// config.toml, deduplicating on read. Called from approval.Cache when the user
// picks AllowAlways at a prompt. Missing config file is created.
func PersistAllowlistEntry(key string) error {
	if key == "" {
		return nil
	}
	path := config.ConfigPath()
	root, err := readTOMLTreeApproval(path)
	if err != nil {
		return err
	}
	sb, _ := root["sandbox"].(map[string]any)
	if sb == nil {
		sb = map[string]any{}
	}
	existing, _ := sb["command_allowlist"].([]any)
	for _, e := range existing {
		if s, ok := e.(string); ok && s == key {
			return nil // already present
		}
	}
	existing = append(existing, key)
	sb["command_allowlist"] = existing
	root["sandbox"] = sb
	return atomicWriteTOMLApproval(path, root)
}

func readTOMLTreeApproval(path string) (map[string]any, error) {
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

func atomicWriteTOMLApproval(path string, root map[string]any) error {
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
