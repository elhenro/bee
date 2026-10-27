package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/flock"
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
	// same lock the settings picker takes, so toggles and grants don't lose each other's write
	defer flock.Lock(path)()
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
	if hasString(existing, key) {
		return nil // already present
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
	return hasString(list, key)
}

func hasString(list []any, key string) bool {
	return slices.ContainsFunc(list, func(e any) bool { s, ok := e.(string); return ok && s == key })
}

func quoteTOML(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// atomicWriteApproval writes config.toml through atomicfile, keeping an
// existing file's mode (0600 for a new one).
func atomicWriteApproval(path string, out []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return atomicfile.Write(path, out, mode)
}

// persistVisionModel records the user's answer to "does this model accept
// images?" as [vision.models] "<model>" = <bool> in config.toml, keeping every
// other key. Missing file is created.
func persistVisionModel(model string, hasVision bool) error {
	if model == "" {
		return nil
	}
	path := config.ConfigPath()
	defer flock.Lock(path)()
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}
	vis, _ := root["vision"].(map[string]any)
	if vis == nil {
		vis = map[string]any{}
	}
	models, _ := vis["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}
	models[model] = hasVision
	vis["models"] = models
	root["vision"] = vis
	out, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal toml: %w", err)
	}
	return atomicWriteApproval(path, out)
}
