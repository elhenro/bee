package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 2-char tags (go, ui, db) must match whole query words.
func TestQueryShortTagMatches(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: gonotes\ndescription: notes\ntags: [go]\npriority: 3\n---\n\nbody\n"
	writeRaw(t, dir, "gonotes", body)
	// equal priority, and the other record is newer, so only the tag can win
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "gonotes.md"), old, old); err != nil {
		t.Fatal(err)
	}
	writeStoreRec(t, dir, "other")
	recs, err := Query(context.Background(), dir, "fix the go build", 5, Options{})
	if err != nil || len(recs) != 2 {
		t.Fatalf("query = %d recs, err %v", len(recs), err)
	}
	if recs[0].Name != "gonotes" {
		t.Fatalf("tag %q did not boost its record: first = %s", "go", recs[0].Name)
	}
}

// a bare date means the record lives through that whole day.
func TestExpiresDateOnlyKeepsRecordAllDay(t *testing.T) {
	got, err := parseExpires("2026-10-19")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 19, 23, 59, 59, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseExpires(date) = %v, want %v", got, want)
	}
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	writeRaw(t, dir, "x", "---\nname: x\ndescription: d\nexpires: "+today+"\n---\n\nb\n")
	recs, _ := Query(context.Background(), dir, "anything", 5, Options{Now: time.Now().UTC().Truncate(24 * time.Hour).Add(time.Hour)})
	if len(recs) != 1 {
		t.Fatalf("record expiring today dropped early: %d recs", len(recs))
	}
}

// values that look like YAML syntax must survive a write/read round trip.
func TestWriteRecordRoundTripsYAMLLookalikes(t *testing.T) {
	for _, desc := range []string{"- leading dash item", "? question", "-", "null", "~", "yes", "123"} {
		dir := t.TempDir()
		p, err := WriteRecord(dir, Record{Entry: Entry{Name: "r", Description: desc, Tags: []string{"x"}, Priority: 5}, Body: "b"})
		if err != nil {
			t.Fatalf("%q: %v", desc, err)
		}
		e, err := ReadEntry(p)
		if err != nil {
			t.Fatal(err)
		}
		if e.Description != desc || e.Priority != 5 || len(e.Tags) != 1 {
			t.Errorf("%q round-tripped to desc=%q prio=%d tags=%v", desc, e.Description, e.Priority, e.Tags)
		}
	}
}
