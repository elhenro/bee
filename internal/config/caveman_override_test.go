package config_test

import (
	"testing"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
)

// Tiny profile defaults Caveman:"full". Verify explicit opt-out (e.g. via
// `--caveman off`) survives ApplyProfile so user can disable on small models.
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

func TestAutoCavemanFollowsTinyProfile(t *testing.T) {
	c := config.Defaults()
	c.Profile = "tiny"
	c.Caveman = "auto"
	c = config.ApplyProfile(c)
	if c.Caveman != "full" {
		t.Fatalf("auto on tiny: got %q want full", c.Caveman)
	}
}
