// Package bundled embeds default skills shipped with the bee binary.
//
// On first run (or whenever the user's skills dir is empty / missing
// individual default skills) WriteDefaults copies the bundled markdown
// files into the user's skills directory. User edits are preserved:
// existing files are never overwritten.
package bundled

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed *.md
var fsys embed.FS

// Files lists the bundled skill filenames (without dir).
func Files() ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// seededFile records every bundled name already offered to this skills dir,
// so a skill the user deleted is not resurrected on the next start.
const seededFile = ".bundled-seeded"

func readSeeded(skillsDir string) map[string]bool {
	seeded := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(skillsDir, seededFile))
	if err != nil {
		return seeded
	}
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			seeded[l] = true
		}
	}
	return seeded
}

// WriteDefaults copies bundled skills into skillsDir if they don't already
// exist and were never seeded before. Existing user files are left untouched,
// deleted ones stay deleted. Returns the list of filenames actually written.
func WriteDefaults(skillsDir string) ([]string, error) {
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", skillsDir, err)
	}
	names, err := Files()
	if err != nil {
		return nil, fmt.Errorf("list bundled: %w", err)
	}
	seeded := readSeeded(skillsDir)
	changed := false
	defer func() {
		if !changed {
			return
		}
		var keys []string
		for k := range seeded {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		_ = os.WriteFile(filepath.Join(skillsDir, seededFile), []byte(strings.Join(keys, "\n")+"\n"), 0o644)
	}()
	var written []string
	for _, n := range names {
		dst := filepath.Join(skillsDir, n)
		if _, err := os.Stat(dst); err == nil {
			// preserve user edits; record pre-manifest files as seeded
			if !seeded[n] {
				seeded[n], changed = true, true
			}
			continue
		} else if !os.IsNotExist(err) {
			return written, fmt.Errorf("stat %s: %w", dst, err)
		}
		if seeded[n] {
			continue // user deleted it
		}
		data, err := fsys.ReadFile(n)
		if err != nil {
			return written, fmt.Errorf("read embed %s: %w", n, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", dst, err)
		}
		seeded[n], changed = true, true
		written = append(written, n)
	}
	return written, nil
}
