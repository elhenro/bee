package config_test

import (
	"testing"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
)

// Tiny profile defaults Caveman:"off". Verify explicit value (e.g. via flag)
// survives ApplyProfile so user can force caveman on small models.
func TestExplicitCavemanWinsOverTinyProfile(t *testing.T) {
	c := config.Defaults()
	c.Profile = "tiny"
	c.Caveman = "full" // simulate --caveman full
	c = config.ApplyProfile(c)
	if c.Caveman != "full" {
		t.Fatalf("explicit caveman lost: got %q want full", c.Caveman)
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
	if c.Caveman != "off" {
		t.Fatalf("auto on tiny: got %q want off", c.Caveman)
	}
}
