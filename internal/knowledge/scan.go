package knowledge

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
)

// MaxEntries caps how many entries ScanStore will return per call. larger
// stores still load but the tail is dropped after mtime sort.
const MaxEntries = 200

// storeMu serializes index writes against concurrent scans so a half-written
// INDEX.md or in-progress rename never surfaces as a read error.
var storeMu sync.RWMutex

// FrontmatterMaxLines bounds the byte budget for header parsing. anything
// past this is treated as body.
const FrontmatterMaxLines = 30

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
	info, statErr := os.Stat(dir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, nil
		}
		return nil, statErr
	}
	dirMtime := info.ModTime()
	if cached, ok := lookupScanCache(dir, dirMtime); ok {
		return cached, nil
	}

	paths, err := listFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(paths) == 0 {
		storeScanCache(dir, dirMtime, nil)
		return nil, nil
	}

	entries := make([]Entry, len(paths))
	errs := make([]error, len(paths))

	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()
			if ctx.Err() != nil {
				errs[idx] = ctx.Err()
				return
			}
			e, rerr := ReadEntry(path)
			if rerr != nil {
				errs[idx] = rerr
				return
			}
			entries[idx] = e
		}(i, p)
	}
	wg.Wait()

	out := entries[:0]
	for i, e := range entries {
		if errs[i] != nil || e.Path == "" {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Modified.After(out[j].Modified)
	})
	if len(out) > MaxEntries {
		out = out[:MaxEntries]
	}
	storeScanCache(dir, dirMtime, out)
	return cloneEntries(out), nil
}

func lookupScanCache(dir string, dirMtime time.Time) ([]Entry, bool) {
	scanCacheMu.RLock()
	c, ok := scanCache[dir]
	scanCacheMu.RUnlock()
	if !ok || !c.dirMtime.Equal(dirMtime) {
		return nil, false
	}
	return cloneEntries(c.entries), true
}

func storeScanCache(dir string, dirMtime time.Time, entries []Entry) {
	scanCacheMu.Lock()
	scanCache[dir] = scanCacheEntry{
		dirMtime: dirMtime,
		entries:  cloneEntries(entries),
	}
	scanCacheMu.Unlock()
}

func cloneEntries(in []Entry) []Entry {
	if len(in) == 0 {
		return nil
	}
	out := make([]Entry, len(in))
	copy(out, in)
	return out
}

// ListEntries returns every *.md path under dir except the INDEX. callers
// use it to cheaply count records without firing the parser.
func ListEntries(dir string) ([]string, error) {
	out, err := listFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

func listFiles(dir string) ([]string, error) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range entries {
		// store is flat by design; never recurse into subdirs.
		if d.IsDir() {
			continue
		}
		name := d.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if name == IndexFileName || name == "MEMORY.md" {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out, nil
}

// ReadEntry stats and parses the frontmatter of one file into an Entry.
// missing or unparseable frontmatter degrades to a name-only record so the
// caller still sees the file in listings.
func ReadEntry(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	fm, err := readFrontmatter(path)
	if err != nil {
		return Entry{
			Path:     path,
			Name:     nameFromPath(path),
			Priority: DefaultPriority,
			Modified: info.ModTime(),
		}, nil
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = nameFromPath(path)
	}
	tags := normalizeTags(fm.Tags)
	prio := fm.Priority
	if prio == 0 {
		prio = DefaultPriority
	}
	if legacy := strings.TrimSpace(strings.ToLower(fm.LegacyType)); legacy != "" && len(tags) == 0 {
		tag, lp := tagFromLegacyType(legacy)
		if tag != "" {
			tags = []string{tag}
		}
		if fm.Priority == 0 && lp > 0 {
			prio = lp
		}
	}
	expiresAt := time.Time{}
	if exp := strings.TrimSpace(fm.Expires); exp != "" && !strings.EqualFold(exp, "never") {
		if t, perr := parseExpires(exp); perr == nil {
			expiresAt = t
		}
	}
	return Entry{
		Path:        path,
		Name:        name,
		Description: strings.TrimSpace(fm.Description),
		Tags:        tags,
		Priority:    prio,
		ExpiresAt:   expiresAt,
		Modified:    info.ModTime(),
	}, nil
}

func nameFromPath(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// tagFromLegacyType maps the deprecated four-value type field onto one of
// the well-known tag names plus a sensible default priority.
func tagFromLegacyType(t string) (string, int) {
	switch t {
	case "user":
		return TagPersonal, DefaultPriority
	case "feedback":
		return TagGuidance, 5
	case "project":
		return TagProject, DefaultPriority
	case "reference":
		return TagExternal, 2
	}
	return "", 0
}

func normalizeTags(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseExpires accepts RFC3339 timestamps and plain `YYYY-MM-DD` dates.
func parseExpires(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized expires value %q", s)
}

// readFrontmatter reads up to FrontmatterMaxLines and parses the YAML block
// fenced by `---`. anything missing or malformed surfaces as an error so
// the caller can degrade to a name-only entry.
func readFrontmatter(path string) (frontmatter, error) {
	f, err := os.Open(path)
	if err != nil {
		return frontmatter{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 512*1024)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) >= FrontmatterMaxLines {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return frontmatter{}, err
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return frontmatter{}, fmt.Errorf("no frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return frontmatter{}, fmt.Errorf("unterminated frontmatter")
	}
	body := strings.Join(lines[1:end], "\n")
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(body), &fm); err != nil {
		return frontmatter{}, err
	}
	return fm, nil
}

// Body returns the record's content with the frontmatter stripped.
func Body(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return s, nil
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return "", nil
	}
	rest := s[nl+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return rest, nil
	}
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimPrefix(body, "\r\n")
	return body, nil
}
