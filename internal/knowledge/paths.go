package knowledge

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// slug regex: collapse anything non-alphanumeric to a dash so a path can
// safely be used as a single directory name.
var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// maxSlugLen keeps a per-segment name below typical FS limits even with
// the hash suffix appended.
const maxSlugLen = 200

// driveLike refuses windows-style near-root inputs (`C:`).
var driveLike = regexp.MustCompile(`^[A-Za-z]:$`)

// StoreDirName is the leaf directory bee writes records into.
const StoreDirName = "store"

// IndexFileName is the rebuilt-on-write index inside StoreDirName. it is
// excluded from scans so the index entry never appears as a record.
const IndexFileName = "INDEX.md"

// envOverride lets callers force a specific store path.
const envOverride = "BEE_STORE_DIR"

// legacyStoreLeaf is the pre-rewrite directory name. retained so on-disk
// stores written before the rewrite still resolve cleanly.
const legacyStoreLeaf = "memory"

// StoreDir returns the directory bee will read/write knowledge records to.
//
// resolution order:
//  1. $BEE_STORE_DIR (must be absolute + non-near-root)
//  2. if cwd is inside a git repo: ~/.bee/store/<slug-of-git-root>
//  3. ~/.bee/store
//
// when only the legacy path exists ( ~/.bee/projects/<slug>/memory or
// ~/.bee/memory ) StoreDir returns it as-is so existing data still loads.
func StoreDir() (string, error) {
	if raw := os.Getenv(envOverride); raw != "" {
		p, ok := validateStorePath(raw)
		if !ok {
			return "", errors.New(envOverride + " rejected: must be absolute and not near-root")
		}
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".bee")

	if git, ok := findGitRoot(); ok {
		slug := slugifyPath(git)
		modern := filepath.Join(root, StoreDirName, slug)
		if dirExists(modern) {
			return modern, nil
		}
		legacy := filepath.Join(root, "projects", slug, legacyStoreLeaf)
		if dirExists(legacy) {
			return legacy, nil
		}
		return modern, nil
	}
	modern := filepath.Join(root, StoreDirName)
	if dirExists(modern) {
		return modern, nil
	}
	legacy := filepath.Join(root, legacyStoreLeaf)
	if dirExists(legacy) {
		return legacy, nil
	}
	return modern, nil
}

// Index returns the INDEX.md path inside StoreDir.
func Index() (string, error) {
	b, err := StoreDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(b, IndexFileName), nil
}

// findGitRoot returns the canonical git root for cwd if any. symlinks are
// resolved so worktrees that link back to the same repo collapse.
func findGitRoot() (string, bool) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p, true
}

// slugifyPath turns an arbitrary path into one safe directory name. long
// inputs are truncated and given a djb2 suffix so distinct roots remain
// distinct after truncation.
func slugifyPath(name string) string {
	s := nonAlnum.ReplaceAllString(name, "-")
	if len(s) <= maxSlugLen {
		return s
	}
	return s[:maxSlugLen] + "-" + djb2(name)
}

func djb2(s string) string {
	var h uint64 = 5381
	for i := 0; i < len(s); i++ {
		h = (h * 33) ^ uint64(s[i])
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if h == 0 {
		return "0"
	}
	buf := make([]byte, 0, 13)
	for h > 0 {
		buf = append(buf, digits[h%36])
		h /= 36
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// validateStorePath rejects ambiguous or near-root inputs. tilde leads are
// expanded; trailing separators stripped; NUL bytes refused.
func validateStorePath(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	if strings.ContainsRune(raw, 0) {
		return "", false
	}
	if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		rest := raw[2:]
		cleanRest := filepath.Clean(rest)
		if cleanRest == "." || cleanRest == ".." {
			return "", false
		}
		raw = filepath.Join(home, rest)
	}
	cleaned := filepath.Clean(raw)
	cleaned = strings.TrimRight(cleaned, `/\`)
	if len(cleaned) < 3 {
		return "", false
	}
	if driveLike.MatchString(cleaned) {
		return "", false
	}
	if strings.HasPrefix(cleaned, `\\`) || strings.HasPrefix(cleaned, `//`) {
		return "", false
	}
	if !filepath.IsAbs(cleaned) {
		return "", false
	}
	return cleaned, true
}

func dirExists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
