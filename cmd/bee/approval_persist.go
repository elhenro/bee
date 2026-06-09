package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/elhenro/bee/internal/config"
)

// PersistAllowlistEntry appends key to sandbox.command_allowlist in the user's
// config.toml, deduplicating on read. Called from approval.Cache when the user
// picks AllowAlways at a prompt. Missing config file is created.
//
// The edit is textual: a full unmarshal→marshal roundtrip would strip every
// comment and reorder the user's config. Falls back to the structural rewrite
// only when the surgical insert can't be validated.
func PersistAllowlistEntry(key string) error {
	if key == "" {
		return nil
	}
	path := config.ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	root := map[string]any{}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}
	sb, _ := root["sandbox"].(map[string]any)
	existing, _ := sb["command_allowlist"].([]any)
	for _, e := range existing {
		if s, ok := e.(string); ok && s == key {
			return nil // already present
		}
	}

	if out, ok := insertAllowlistEntry(string(data), key); ok && allowlistContains(out, key) {
		return atomicWriteApproval(path, []byte(out))
	}

	// fallback: structural rewrite. loses comments, but never loses the entry.
	if sb == nil {
		sb = map[string]any{}
	}
	sb["command_allowlist"] = append(existing, key)
	root["sandbox"] = sb
	out, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal toml: %w", err)
	}
	return atomicWriteApproval(path, out)
}

var sandboxHeaderRe = regexp.MustCompile(`(?m)^[ \t]*\[sandbox\][ \t]*(#.*)?$`)
var allowlistKeyRe = regexp.MustCompile(`(?m)^[ \t]*command_allowlist[ \t]*=`)

// insertAllowlistEntry splices key into sandbox.command_allowlist in the raw
// TOML text, preserving everything else byte for byte. ok=false means the
// text didn't match a shape we can edit safely.
func insertAllowlistEntry(text, key string) (string, bool) {
	quoted := quoteTOML(key)

	header := sandboxHeaderRe.FindStringIndex(text)
	if header == nil {
		// no [sandbox] section: append one.
		sep := ""
		if text != "" && !strings.HasSuffix(text, "\n") {
			sep = "\n"
		}
		return text + sep + "\n[sandbox]\ncommand_allowlist = [" + quoted + "]\n", true
	}

	// section body runs until the next table header line.
	bodyStart := header[1]
	bodyEnd := len(text)
	if next := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(text[bodyStart:]); next != nil {
		bodyEnd = bodyStart + next[0]
	}
	body := text[bodyStart:bodyEnd]

	keyLoc := allowlistKeyRe.FindStringIndex(body)
	if keyLoc == nil {
		// section exists but no allowlist yet: add it right under the header.
		return text[:bodyStart] + "\ncommand_allowlist = [" + quoted + "]" + text[bodyStart:], true
	}

	// find the array's opening bracket after '='.
	open := strings.Index(body[keyLoc[1]:], "[")
	if open < 0 {
		return "", false
	}
	insertAt := bodyStart + keyLoc[1] + open + 1

	// empty array gets no trailing comma; prepending (rather than appending)
	// sidesteps comma/comment gymnastics at the closing bracket.
	rest := strings.TrimLeft(text[insertAt:], " \t\r\n")
	entry := quoted + ", "
	if strings.HasPrefix(rest, "]") {
		entry = quoted
	}
	return text[:insertAt] + entry + text[insertAt:], true
}

// allowlistContains re-parses edited text and confirms the key landed where
// load.go will read it. Guards against an insert into a comment or string.
func allowlistContains(text, key string) bool {
	root := map[string]any{}
	if err := toml.Unmarshal([]byte(text), &root); err != nil {
		return false
	}
	sb, _ := root["sandbox"].(map[string]any)
	list, _ := sb["command_allowlist"].([]any)
	for _, e := range list {
		if s, ok := e.(string); ok && s == key {
			return true
		}
	}
	return false
}

func quoteTOML(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func atomicWriteApproval(path string, out []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
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
