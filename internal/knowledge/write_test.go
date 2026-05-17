package knowledge

import (
	"os"
	"path/filepath"
	"strings"
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
