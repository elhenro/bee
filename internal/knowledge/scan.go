package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// MaxEntries caps how many entries ScanStore will return per call. larger
// stores still load but the tail is dropped after mtime sort.
const MaxEntries = 200

// storeMu serializes index writes against concurrent scans so a half-written
// INDEX.md or in-progress rename never surfaces as a read error.
var storeMu sync.RWMutex

// scanCacheEntry holds a parsed snapshot plus the dir mtime at scan time.
// callers receive a defensive copy so mutations cannot poison the cache.
type scanCacheEntry struct {
	dirMtime time.Time
	entries  []Entry
}

var (
	scanCacheMu sync.RWMutex
	scanCache   = map[string]scanCacheEntry{}
)

// invalidateScanCache drops the cached snapshot for dir. write paths call
// this so the next ScanStore rescans regardless of dir mtime resolution.
func invalidateScanCache(dir string) {
	if dir == "" {
		return
	}
	scanCacheMu.Lock()
	delete(scanCache, dir)
	scanCacheMu.Unlock()
}

// ScanStore walks dir for *.md files, parses each frontmatter in parallel,
// and returns entries sorted mtime desc, capped at MaxEntries. INDEX.md is
// excluded. a missing dir returns (nil, nil) so callers can fall through.
// repeat calls return a cached slice when the dir mtime is unchanged.
func ScanStore(ctx context.Context, dir string) ([]Entry, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dirMtime := info.ModTime()
	if cached, ok := lookupScanCache(dir, dirMtime); ok {
		return cached, nil
	}

	paths, err := ListEntries(dir)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Go(func() {
			if ctx.Err() == nil {
				entries[i], _ = ReadEntry(p) // a vanished file leaves a zero Entry
			}
		})
	}
	wg.Wait()
	// a canceled scan is partial; hand back the cancellation, cache nothing.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var out []Entry
	for _, e := range entries {
		if e.Path != "" {
			out = append(out, e)
		}
	}
	complete := len(out) == len(entries)
	slices.SortFunc(out, func(a, b Entry) int { return b.Modified.Compare(a.Modified) })
	out = out[:min(len(out), MaxEntries)]
	// a listing that skipped unreadable files is not authoritative: rescan next time.
	if complete {
		storeScanCache(dir, dirMtime, out)
	}
	return slices.Clone(out), nil
}

func lookupScanCache(dir string, dirMtime time.Time) ([]Entry, bool) {
	scanCacheMu.RLock()
	c, ok := scanCache[dir]
	scanCacheMu.RUnlock()
	if !ok || !c.dirMtime.Equal(dirMtime) {
		return nil, false
	}
	return slices.Clone(c.entries), true
}

func storeScanCache(dir string, dirMtime time.Time, entries []Entry) {
	scanCacheMu.Lock()
	scanCache[dir] = scanCacheEntry{dirMtime: dirMtime, entries: slices.Clone(entries)}
	scanCacheMu.Unlock()
}

// ListEntries returns every *.md path under dir except the INDEX. callers
// use it to cheaply count records without firing the parser. a missing dir
// yields no paths.
func ListEntries(dir string) ([]string, error) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	paths, err := storePaths(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return paths, err
}

// storePaths lists the record files of dir; the caller holds storeMu.
func storePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range entries {
		name := d.Name()
		// store is flat by design; never recurse into subdirs.
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if name == IndexFileName || name == "MEMORY.md" {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out, nil
}
