package router

import "testing"

func TestUserDeleteEmptyID(t *testing.T) {
	known := map[string]bool{"u1": true}
	if got := handleUserDelete("", known); got != StatusBadRequest {
		t.Fatalf("delete empty id = %d want %d", got, StatusBadRequest)
	}
}

func TestUserDeleteKnown(t *testing.T) {
	known := map[string]bool{"u1": true}
	if got := handleUserDelete("u1", known); got != StatusOK {
		t.Fatalf("delete known = %d want %d", got, StatusOK)
	}
}
