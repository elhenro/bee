package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/tools"
)

func TestBuildTools_BrowserFlagControlsBrowserTools(t *testing.T) {
	// fake chrome so DetectChrome passes without launching anything
	dir := t.TempDir()
	fake := filepath.Join(dir, "chrome")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	base := config.Config{}
	base.Browser.ChromePath = fake

	// disabled -> no browser tools
	base.Browser.Enabled = false
	regOff, err := buildToolsForTest(t, base)
	if err != nil {
		t.Fatal(err)
	}
	if hasBrowserTool(regOff.Names()) {
		t.Error("browser tools present when disabled")
	}

	// enabled -> browser tools appear
	base.Browser.Enabled = true
	regOn, err := buildToolsForTest(t, base)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBrowserTool(regOn.Names()) {
		t.Error("browser tools missing when enabled")
	}
}

func hasBrowserTool(names []string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, "browser_") {
			return true
		}
	}
	return false
}

// buildToolsForTest calls appendBrowserTools directly — same function the
// Rebuild closure uses — to verify the browser flag gates the tools without
// requiring a full provider or approver.
func buildToolsForTest(t *testing.T, cfg config.Config) (*tools.Registry, error) {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tl := range appendBrowserTools(nil, cfg, false) {
		if err := reg.Register(tl); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
