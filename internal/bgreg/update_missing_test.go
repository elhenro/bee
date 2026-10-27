package bgreg

import (
	"errors"
	"os"
	"testing"
)

func TestUpdateOnMissingSidecarFailsWithoutCreating(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	called := false
	err := Update("ghost", func(s *Status) error { called = true; return nil })
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want one wrapping os.ErrNotExist", err)
	}
	if called {
		t.Error("fn must not run for a session without a sidecar")
	}
	for _, suffix := range []string{".status.json", ".lock"} {
		p, _ := File("ghost", suffix)
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was created for a missing session", suffix)
		}
	}
}

func TestUpdateCannotResurrectRemovedAgent(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	if err := Write(Status{SessionID: "k", State: StateActive}); err != nil {
		t.Fatal(err)
	}
	if err := Remove("k"); err != nil {
		t.Fatal(err)
	}
	err := Update("k", func(s *Status) error { s.State = StateDone; return nil })
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
	if _, err := Read("k"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("killed agent came back: %v", err)
	}
}

func TestUpdateRemovedDuringFnDoesNotRecreate(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	if err := Write(Status{SessionID: "r", State: StateActive}); err != nil {
		t.Fatal(err)
	}
	err := Update("r", func(s *Status) error {
		s.State = StateDone
		return Remove("r")
	})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
	if _, err := Read("r"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status recreated after Remove: %v", err)
	}
}
