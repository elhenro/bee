package tui

import (
	"testing"
	"time"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

func TestSeedFromEngine_MirrorsConfig(t *testing.T) {
	t.Setenv("BEE_VERBOSE", "")
	t.Setenv("BEE_COMPACT", "")
	t.Setenv("BEE_WATCHDOG_OFF", "")
	cfg := config.Defaults()
	cfg.Verbose, cfg.ShowThoughts, cfg.ShowCwd, cfg.ShowLoaderRate = true, false, false, true
	cfg.OuterPadLR = 99
	cfg.TUI.Loader = "strip"
	cfg.TUI.StatusLine = []string{"model"}
	cfg.Watchdog = config.WatchdogConfig{Enabled: true, StallSeconds: 7, MaxResumes: 5}
	stream := make(chan string, 1)
	eng := &loop.Engine{
		Cfg:             cfg,
		StreamCh:        stream,
		InitialMessages: []types.Message{assistantText("hi")},
	}

	m := NewModel(eng, "/tmp", "m", "", caveman.Default).seedFromEngine(eng)

	if !m.verbose || !m.stream.verbose {
		t.Error("verbose should reach the model and its renderer")
	}
	if m.showThoughts || m.stream.showThoughts {
		t.Error("show_thoughts=false should reach the model and its renderer")
	}
	if m.showCwd || !m.showLoaderRate {
		t.Errorf("chrome toggles not mirrored: showCwd=%v showLoaderRate=%v", m.showCwd, m.showLoaderRate)
	}
	if m.outerPadLR != 8 {
		t.Errorf("outer pad should clamp to 8, got %d", m.outerPadLR)
	}
	if m.loader != "strip" || len(m.statusLine) != 1 {
		t.Errorf("loader/status line not mirrored: %q %v", m.loader, m.statusLine)
	}
	if m.streamCh != stream || len(m.messages) != 1 {
		t.Error("channel and initial messages should be wired")
	}
	if !m.watchdogEnabled || m.watchdogStall != 7*time.Second || m.watchdogMaxResumes != 5 {
		t.Errorf("watchdog not mirrored: %v %v %v", m.watchdogEnabled, m.watchdogStall, m.watchdogMaxResumes)
	}
}

func TestSeedFromEngine_NilEngineDefaults(t *testing.T) {
	t.Setenv("BEE_VERBOSE", "1")
	t.Setenv("BEE_COMPACT", "")
	m := NewModel(nil, "/tmp", "m", "", caveman.Default).seedFromEngine(nil)
	if !m.verbose || m.compact {
		t.Errorf("env should drive verbose/compact without an engine: %v %v", m.verbose, m.compact)
	}
	if !m.showBanner || !m.showLoader || !m.showLoaderIn || !m.showLoaderOut || m.showLoaderRate {
		t.Error("without an engine the banner and loader figures default on, tok/s off")
	}
}
