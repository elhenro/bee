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

// WriteDefaults copies bundled skills into skillsDir if they don't already
// exist. Existing user files are left untouched. Returns the list of
// filenames actually written.
func WriteDefaults(skillsDir string) ([]string, error) {
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", skillsDir, err)
	}
	names, err := Files()
	if err != nil {
		return nil, fmt.Errorf("list bundled: %w", err)
	}
	var written []string
	for _, n := range names {
		dst := filepath.Join(skillsDir, n)
		if _, err := os.Stat(dst); err == nil {
			continue // preserve user edits
		} else if !os.IsNotExist(err) {
			return written, fmt.Errorf("stat %s: %w", dst, err)
		}
		data, err := fsys.ReadFile(n)
		if err != nil {
			return written, fmt.Errorf("read embed %s: %w", n, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", dst, err)
		}
		written = append(written, n)
	}
	return written, nil
}
