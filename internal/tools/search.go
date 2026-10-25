package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skipDirs are never descended by the recursive search tools. .claude holds
// agent state (worktrees with duplicate code) that would double-count files.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "testdata": true, ".claude": true}

// ResolveSearchRoot picks the directory a recursive search starts in. confined
// (the tool's non-empty root) contains it to the workspace so a model path
// can't escape (e.g. path:~/.ssh); danger-full-access (empty root) searches
// anywhere, defaulting to cwd. An empty path means the tool's root.
func ResolveSearchRoot(confined, path string) (string, error) {
	if path == "" {
		path = confined
	}
	if confined != "" {
		abs, _, rootAbs, ok := ResolveInRoot(confined, path)
		if !ok {
			return "", fmt.Errorf("path %q escapes workspace root %q", path, rootAbs)
		}
		return abs, nil
	}
	if path == "" {
		if wd, err := os.Getwd(); err == nil {
			path = wd
		}
	}
	if a, err := filepath.Abs(path); err == nil {
		path = a
	}
	return path, nil
}

// WalkFiles calls fn for every non-directory under root, skipping noise
// directories below it (never root itself). Unreadable entries are skipped;
// fn returns filepath.SkipAll to stop early, which is not reported as an error.
func WalkFiles(ctx context.Context, root string, fn func(path string) error) error {
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(p)
	})
	if errors.Is(err, filepath.SkipAll) {
		return nil
	}
	return err
}

// LinesResult joins search hits into a result, or reports no matches.
func LinesResult(lines []string) Result {
	if len(lines) == 0 {
		return Result{Content: "no matches"}
	}
	return Result{Content: strings.Join(lines, "\n")}
}
