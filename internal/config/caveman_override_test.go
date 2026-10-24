package config_test

import (
	"testing"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
)

// Profiles default Caveman:"off". Verify an explicit choice (e.g. via
// `--caveman off`) survives ApplyProfile.
func TestExplicitCavemanWinsOverTinyProfile(t *testing.T) {
	c := config.Defaults()
	c.Profile = "tiny"
	c.Caveman = "off" // simulate --caveman off
	c = config.ApplyProfile(c)
	if c.Caveman != "off" {
		t.Fatalf("explicit caveman lost: got %q want off", c.Caveman)
	}
	if _, err := caveman.ParseLevel(c.Caveman); err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
}

func TestAutoCavemanOffOnTinyProfile(t *testing.T) {
	c := config.Defaults()
	c.Profile = "tiny"
	c.Caveman = "auto"
	c = config.ApplyProfile(c)
	if c.Caveman != "off" {
		t.Fatalf("auto on tiny: got %q want off", c.Caveman)
	}
}
