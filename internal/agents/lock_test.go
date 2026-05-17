package agents

import (
	"testing"
)

func TestAcquireLock_AcquireAndRelease(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	l, ok, err := AcquireLock()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true on first acquire")
	}
	defer l.Release()

	// second acquire while held should refuse
	l2, ok2, err := AcquireLock()
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if ok2 {
		t.Fatal("expected ok=false while held")
	}
	if l2 != nil {
		t.Fatal("expected nil lock when refused")
	}

	l.Release()

	// after release, acquire again
	l3, ok3, err := AcquireLock()
	if err != nil {
		t.Fatalf("third acquire: %v", err)
	}
	if !ok3 {
		t.Fatal("expected ok=true after release")
	}
	l3.Release()
}

func TestParseConflictFiles(t *testing.T) {
	in := "CONFLICT (content): Merge conflict in foo.go\nsome other line\nCONFLICT (content): Merge conflict in bar/baz.go\n"
	got := parseConflictFiles(in)
	if len(got) != 2 || got[0] != "foo.go" || got[1] != "bar/baz.go" {
		t.Fatalf("got %v", got)
	}
}
