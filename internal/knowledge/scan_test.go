package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanFixtureStore(t *testing.T) {
	dir := "testdata/store"
	entries, err := ScanStore(context.Background(), dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("want 4 entries, got %d", len(entries))
	}
	byName := map[string]Entry{}
	for _, e := range entries {
		byName[filepath.Base(e.Path)] = e
	}
	if _, has := byName["INDEX.md"]; has {
		t.Fatal("INDEX.md must be excluded")
	}
	if got := byName["user-role.md"].Tags; !contains(got, "personal") {
		t.Errorf("user-role tags missing personal: %v", got)
	}
	if got := byName["feedback-testing.md"].Priority; got != 5 {
		t.Errorf("feedback-testing priority want 5 got %d", got)
	}
	legacy := byName["legacy-type-user.md"]
	if !contains(legacy.Tags, TagPersonal) {
		t.Errorf("legacy type=user should migrate to %q tag: %v", TagPersonal, legacy.Tags)
	}
	bare := byName["legacy-no-tags.md"]
	if bare.Priority != DefaultPriority {
		t.Errorf("bare legacy should default priority: %d", bare.Priority)
	}
	if len(bare.Tags) != 0 {
		t.Errorf("bare legacy should have no tags: %v", bare.Tags)
	}
}

func TestScanSortedByMtimeDesc(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		body := "---\nname: " + strings.TrimSuffix(name, ".md") + "\ndescription: x\ntags: [misc]\npriority: 3\nexpires: never\n---\n"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-age)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.md", 5*time.Hour)
	mk("b.md", 1*time.Hour)
	mk("c.md", 24*time.Hour)
	entries, err := ScanStore(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b.md", "a.md", "c.md"}
	for i, e := range entries {
		if got := filepath.Base(e.Path); got != want[i] {
			t.Errorf("idx %d: want %s got %s", i, want[i], got)
		}
	}
}

func TestScanCapsAtMaxEntries(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < MaxEntries+20; i++ {
		p := filepath.Join(dir, "m"+itoa3(i)+".md")
		body := "---\nname: m\ndescription: d\ntags: [misc]\npriority: 3\nexpires: never\n---\n"
		os.WriteFile(p, []byte(body), 0o644)
	}
	entries, err := ScanStore(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MaxEntries {
		t.Fatalf("want %d got %d", MaxEntries, len(entries))
	}
}

func TestScanStoreCacheHit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	body := "---\nname: a\ndescription: d\ntags: [misc]\npriority: 3\nexpires: never\n---\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := ScanStore(context.Background(), dir)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("want 1 entry, got %d", len(first))
	}
	// remove file on disk: a cache miss would now return zero entries,
	// while a cache hit returns the original snapshot.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	// dir mtime would update on remove. force the cache mtime to match
	// the post-remove dir so the lookup hits.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	scanCacheMu.Lock()
	c := scanCache[dir]
	c.dirMtime = info.ModTime()
	scanCache[dir] = c
	scanCacheMu.Unlock()
	second, err := ScanStore(context.Background(), dir)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("cache hit expected to return cached entry, got %d", len(second))
	}
	// mutating the returned slice must not poison the cache.
	second[0].Name = "mutated"
	third, _ := ScanStore(context.Background(), dir)
	if len(third) != 1 || third[0].Name == "mutated" {
		t.Fatalf("cache returned aliased slice: %+v", third)
	}
	// explicit invalidation forces a rescan: file is gone, so empty.
	invalidateScanCache(dir)
	fourth, err := ScanStore(context.Background(), dir)
	if err != nil {
		t.Fatalf("fourth scan: %v", err)
	}
	if len(fourth) != 0 {
		t.Fatalf("post-invalidate want 0 entries, got %d", len(fourth))
	}
}

func TestScanMissingDir(t *testing.T) {
	entries, err := ScanStore(context.Background(), "testdata/does-not-exist")
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if entries != nil {
		t.Fatalf("want nil, got %v", entries)
	}
}

// regression: scan must treat the store as flat and skip nested directories
// even if they contain .md files.
func TestScanIgnoresSubdirs(t *testing.T) {
	dir := t.TempDir()
	top := "---\nname: top\ndescription: d\ntags: [misc]\npriority: 3\nexpires: never\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "top.md"), []byte(top), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "deep.md"), []byte(top), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := ScanStore(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Base(entries[0].Path) != "top.md" {
		t.Fatalf("scan recursed into subdir: %+v", entries)
	}
}

func TestReadEntryParsesExpires(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "soon.md")
	body := "---\nname: soon\ndescription: d\ntags: [misc]\npriority: 4\nexpires: 2030-01-01\n---\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := ReadEntry(p)
	if err != nil {
		t.Fatal(err)
	}
	if e.ExpiresAt.IsZero() {
		t.Errorf("expires not parsed")
	}
	if e.Priority != 4 {
		t.Errorf("priority lost: %d", e.Priority)
	}
}

func TestBodyStripsFrontmatter(t *testing.T) {
	body, err := Body("testdata/store/feedback-testing.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(body, "---") {
		t.Fatalf("body should not start with frontmatter: %q", body)
	}
	if !strings.Contains(body, "**Why:**") {
		t.Fatalf("body missing Why line: %q", body)
	}
}

func itoa3(n int) string {
	d := []byte{byte('0' + n/100%10), byte('0' + n/10%10), byte('0' + n%10)}
	return string(d)
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
