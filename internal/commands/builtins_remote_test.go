package commands

import (
	"context"
	"strings"
	"testing"
)

func TestBuiltin_RemoteControl_Fallback(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	c, ok := r.Get("remote-control")
	if !ok {
		t.Fatal("remote-control not registered")
	}
	out, err := c.Run(context.Background(), nil, &fakeSide{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("expected non-empty guidance text")
	}
	if !strings.Contains(out, "bee remote-control") {
		t.Errorf("guidance should mention the command, got %q", out)
	}
}
