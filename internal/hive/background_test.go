package hive

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadPid(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("BEE_HOME", tmp)

	if err := WritePid("sess-1", 12345); err != nil {
		t.Fatalf("WritePid: %v", err)
	}
	pid, err := ReadPid("sess-1")
	if err != nil {
		t.Fatalf("ReadPid: %v", err)
	}
	if pid != 12345 {
		t.Fatalf("pid: got %d want 12345", pid)
	}

	// missing pidfile
	_, err = ReadPid("sess-2")
	if err == nil {
		t.Fatal("expected error for missing pidfile")
	}
}

func TestLogDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("BEE_HOME", tmp)

	dir, err := LogDir()
	if err != nil {
		t.Fatalf("LogDir: %v", err)
	}
	want := filepath.Join(tmp, "sessions", "bg")
	if dir != want {
		t.Fatalf("LogDir: got %q want %q", dir, want)
	}
}

func TestEnsureLogDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("BEE_HOME", tmp)

	p, err := EnsureLogDir("abc")
	if err != nil {
		t.Fatalf("EnsureLogDir: %v", err)
	}
	want := filepath.Join(tmp, "sessions", "bg", "abc.log")
	if p != want {
		t.Fatalf("path: got %q want %q", p, want)
	}
	if _, err := os.Stat(filepath.Join(tmp, "sessions", "bg")); err != nil {
		t.Fatalf("dir not created: %v", err)
	}
}
