package loop

import "testing"

func TestDuplicateWriteTracker_FlagsImmediateDup(t *testing.T) {
	tr := newDuplicateWriteTracker()
	if tr.ObserveWrite("a.go", "x") {
		t.Fatalf("first write must not be dup")
	}
	if !tr.ObserveWrite("a.go", "x") {
		t.Fatalf("second identical write must be dup")
	}
}

func TestDuplicateWriteTracker_ReadClearsDup(t *testing.T) {
	tr := newDuplicateWriteTracker()
	tr.ObserveWrite("a.go", "x")
	tr.ObserveRead("a.go")
	if tr.ObserveWrite("a.go", "x") {
		t.Fatalf("read between writes must clear dup flag")
	}
}

func TestDuplicateWriteTracker_DifferentContentNotDup(t *testing.T) {
	tr := newDuplicateWriteTracker()
	tr.ObserveWrite("a.go", "x")
	if tr.ObserveWrite("a.go", "y") {
		t.Fatalf("changed body must not be dup")
	}
}

func TestDuplicateWriteTracker_DifferentPathNotDup(t *testing.T) {
	tr := newDuplicateWriteTracker()
	tr.ObserveWrite("a.go", "x")
	if tr.ObserveWrite("b.go", "x") {
		t.Fatalf("different path must not be dup")
	}
}
