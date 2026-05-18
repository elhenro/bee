package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := Record{
		Entry: Entry{
			Name:        "test-user-role",
			Description: "test user is a Go dev",
			Tags:        []string{"personal", "identity"},
			Priority:    4,
		},
		Body: "Body line one.\nLine two.",
	}
	path, err := WriteRecord(dir, r)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if filepath.Base(path) != "test-user-role.md" {
		t.Errorf("unexpected filename: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("missing opening frontmatter: %s", s)
	}
	if !strings.Contains(s, "tags: [personal, identity]") {
		t.Errorf("tags missing: %s", s)
	}
	if !strings.Contains(s, "priority: 4") {
		t.Errorf("priority missing: %s", s)
	}
	if !strings.Contains(s, "expires: never") {
		t.Errorf("expires missing: %s", s)
	}
	if !strings.Contains(s, "Body line one.") {
		t.Errorf("body missing: %s", s)
	}
	e, err := ReadEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "test-user-role" || e.Priority != 4 {
		t.Errorf("round-trip mismatch: %+v", e)
	}
	if !contains(e.Tags, "personal") {
		t.Errorf("tags lost on round-trip: %v", e.Tags)
	}
}

func TestWriteValidates(t *testing.T) {
	dir := t.TempDir()
	cases := []Record{
		{Entry: Entry{Name: "", Description: "d"}},
		{Entry: Entry{Name: "ok", Description: ""}},
		{Entry: Entry{Name: "bad name", Description: "d"}},
		{Entry: Entry{Name: "ok", Description: "d", Priority: 6}},
		{Entry: Entry{Name: "ok", Description: "d", Tags: []string{"BadCase"}}},
	}
	for i, r := range cases {
		if _, err := WriteRecord(dir, r); err == nil {
			t.Errorf("case %d: want error, got nil for %+v", i, r.Entry)
		}
	}
}

// regression: a leading dot would shadow system files (.gitconfig.md) in the
// store dir; validation must reject those names outright.
func TestWriteRejectsLeadingDot(t *testing.T) {
	dir := t.TempDir()
	cases := []string{".gitconfig", ".env", ".hidden"}
	for _, name := range cases {
		_, err := WriteRecord(dir, Record{
			Entry: Entry{Name: name, Description: "d"},
			Body:  "body",
		})
		if err == nil {
			t.Errorf("name %q: want error, got nil", name)
		}
		if _, statErr := os.Stat(filepath.Join(dir, name+".md")); statErr == nil {
			t.Errorf("name %q: file should not have been written", name)
		}
	}
}

func TestWriteWithExpiresRFC3339(t *testing.T) {
	dir := t.TempDir()
	exp := time.Date(2030, 6, 1, 12, 0, 0, 0, time.UTC)
	_, err := WriteRecord(dir, Record{
		Entry: Entry{
			Name:        "future",
			Description: "expires in 2030",
			Tags:        []string{"misc"},
			Priority:    3,
			ExpiresAt:   exp,
		},
		Body: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "future.md"))
	if !strings.Contains(string(data), "2030-06-01") {
		t.Errorf("RFC3339 expires missing in %s", string(data))
	}
}

func TestRebuildIndexTable(t *testing.T) {
	dir := t.TempDir()
	must := func(name, desc string, prio int, tags ...string) {
		_, err := WriteRecord(dir, Record{
			Entry: Entry{
				Name:        name,
				Description: desc,
				Tags:        tags,
				Priority:    prio,
			},
			Body: "body",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	must("aaa", "first", 3, "misc")
	must("zzz", "last", 5, "important")

	idx, err := os.ReadFile(filepath.Join(dir, IndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(idx)
	if !strings.Contains(s, "| name | tags | pri | desc |") {
		t.Errorf("expected table header, got: %s", s)
	}
	// zzz has higher priority and must precede aaa
	z := strings.Index(s, "| zzz |")
	a := strings.Index(s, "| aaa |")
	if z < 0 || a < 0 || z > a {
		t.Errorf("index not priority-sorted: %s", s)
	}
}

// regression: parallel WriteRecord + ScanStore callers must not corrupt
// each other; run under `go test -race` to verify.
func TestConcurrentWritesAndScans(t *testing.T) {
	dir := t.TempDir()
	const writers = 4
	const reads = 8
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				name := fmt.Sprintf("rec-%d-%d", idx, j)
				_, err := WriteRecord(dir, Record{
					Entry: Entry{Name: name, Description: "d", Tags: []string{"misc"}, Priority: 3},
					Body:  "body",
				})
				if err != nil {
					t.Errorf("write %s: %v", name, err)
				}
			}
		}(i)
	}
	for i := 0; i < reads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ScanStore(context.Background(), dir); err != nil {
				t.Errorf("scan: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestYAMLEscape(t *testing.T) {
	cases := map[string]string{
		"plain":         "plain",
		"":              `""`,
		"has: colon":    `"has: colon"`,
		`has "quote"`:   `"has \"quote\""`,
		"with\nnewline": `"with\nnewline"`,
	}
	for in, want := range cases {
		if got := yamlEscape(in); got != want {
			t.Errorf("yamlEscape(%q): want %q got %q", in, want, got)
		}
	}
}
