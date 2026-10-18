package session

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPruneEmpty(t *testing.T) {
	pruneEmptyOnce = sync.Once{}
	dir := t.TempDir()
	mk := func(name, body string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-age)
		_ = os.Chtimes(p, past, past)
		return p
	}
	oldEmpty := mk("a.jsonl", "", 2*emptyMaxAge)
	freshEmpty := mk("b.jsonl", "", time.Minute)
	oldFull := mk("c.jsonl", "{}\n", 2*emptyMaxAge)
	own := mk("d.jsonl", "", 2*emptyMaxAge)
	pruneEmpty(dir, own)
	for p, want := range map[string]bool{oldEmpty: false, freshEmpty: true, oldFull: true, own: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", filepath.Base(p), err == nil, want)
		}
	}
}
