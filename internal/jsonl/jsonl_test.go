package jsonl

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendCreatesDirsAndKeepsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "log.jsonl")
	type rec struct{ N int }
	for i := 1; i <= 3; i++ {
		if err := Append(path, rec{N: i}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		var r rec
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil || r.N != n+1 {
			t.Fatalf("line %d = %q (%v)", n+1, sc.Text(), err)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("got %d lines, want 3", n)
	}
}

func TestAppendRejectsUnmarshalable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if err := Append(path, make(chan int)); err == nil {
		t.Fatal("want marshal error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed append must not create the file: %v", err)
	}
}
