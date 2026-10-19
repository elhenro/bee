package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeStoreRec(t *testing.T, dir, name string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: d\npriority: 3\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRaw(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// a scan cut short by cancellation must not poison the cache with a partial
// (here empty) listing.
func TestScanStoreCanceledDoesNotCache(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		writeStoreRec(t, dir, n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanStore(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan err = %v, want context.Canceled", err)
	}
	got, err := ScanStore(context.Background(), dir)
	if err != nil || len(got) != 3 {
		t.Fatalf("fresh scan = %d entries, err %v; want 3", len(got), err)
	}
}

// a file that could not be read is skipped but the listing is not cached, so
// the next scan picks it up once readable (dir mtime does not change).
func TestScanStoreUnreadableEntryNotCached(t *testing.T) {
	dir := t.TempDir()
	writeStoreRec(t, dir, "a")
	target := filepath.Join(t.TempDir(), "late.md")
	if err := os.Symlink(target, filepath.Join(dir, "late.md")); err != nil {
		t.Skip("symlinks unavailable")
	}
	got, err := ScanStore(context.Background(), dir)
	if err != nil || len(got) != 1 {
		t.Fatalf("first scan = %d entries, err %v; want 1", len(got), err)
	}
	if err := os.WriteFile(target, []byte("---\nname: late\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ScanStore(context.Background(), dir)
	if err != nil || len(got) != 2 {
		t.Fatalf("second scan = %d entries, err %v; want 2", len(got), err)
	}
}
