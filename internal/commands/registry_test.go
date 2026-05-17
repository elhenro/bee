package commands

import (
	"context"
	"testing"
)

func TestRegistry_RegisterGet(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "ping", Description: "pong"})
	c, ok := r.Get("ping")
	if !ok {
		t.Fatal("not found")
	}
	if c.Description != "pong" {
		t.Errorf("got %q", c.Description)
	}
}

func TestRegistry_List_Sorted(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "b"})
	r.Register(Command{Name: "a"})
	r.Register(Command{Name: "c"})
	got := r.List()
	if len(got) != 3 {
		t.Fatalf("want 3 got %d", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "c" {
		t.Errorf("not sorted: %v", got)
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get("nope"); ok {
		t.Error("expected not found")
	}
}

func TestCommand_Run_ToLLM(t *testing.T) {
	c := Command{Run: func(_ context.Context, _ []string, _ Side) (string, error) {
		return "hello", nil
	}}
	out, err := c.Run(context.Background(), nil, nil)
	if err != nil || out != "hello" {
		t.Errorf("got %q %v", out, err)
	}
}

func TestRegistry_RegisterOverwrites(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "x", Description: "v1"})
	r.Register(Command{Name: "x", Description: "v2"})
	c, _ := r.Get("x")
	if c.Description != "v2" {
		t.Errorf("expected overwrite, got %q", c.Description)
	}
}
