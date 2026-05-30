package commands

import (
	"context"
	"strings"
	"testing"
)

func TestBrowserCommand_OnOffBare(t *testing.T) {
	r := NewRegistry()
	registerBrowser(r)
	cmd, ok := r.Get("browser")
	if !ok {
		t.Fatal("browser command not registered")
	}

	fs := &browserFakeSide{}
	// /browser on
	out, err := cmd.Run(context.Background(), []string{"on"}, fs)
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	if !fs.lastOn || !strings.Contains(out, "enabled") {
		t.Errorf("on: lastOn=%v out=%q", fs.lastOn, out)
	}
	// /browser off
	out, err = cmd.Run(context.Background(), []string{"off"}, fs)
	if err != nil {
		t.Fatalf("off: %v", err)
	}
	if fs.lastOn || !strings.Contains(out, "disabled") {
		t.Errorf("off: lastOn=%v out=%q", fs.lastOn, out)
	}
	// bare /browser -> status/usage, must NOT call SetBrowserEnabled
	fs.calls = 0
	if _, err := cmd.Run(context.Background(), nil, fs); err != nil {
		t.Fatalf("bare: %v", err)
	}
	if fs.calls != 0 {
		t.Errorf("bare /browser must not toggle, calls=%d", fs.calls)
	}
}

func TestBrowserCommand_BadArg(t *testing.T) {
	r := NewRegistry()
	registerBrowser(r)
	cmd, _ := r.Get("browser")
	out, err := cmd.Run(context.Background(), []string{"banana"}, &browserFakeSide{})
	if err == nil && !strings.Contains(out, "usage") {
		t.Errorf("bad arg should error or show usage, got %q", out)
	}
}

type browserFakeSide struct {
	fakeSide // embed the existing test fake to satisfy all Side methods
	lastOn   bool
	calls    int
}

func (f *browserFakeSide) SetBrowserEnabled(on bool) (string, error) {
	f.calls++
	f.lastOn = on
	if on {
		return "browser tools enabled (5 tools)", nil
	}
	return "browser tools disabled", nil
}
