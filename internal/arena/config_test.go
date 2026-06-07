package arena

import "testing"

func TestDefaultConfigIsSane(t *testing.T) {
	c := DefaultConfig()
	if c.StartTokens <= 0 {
		t.Fatalf("StartTokens = %d, want > 0", c.StartTokens)
	}
	if c.Rounds <= 0 {
		t.Fatalf("Rounds = %d, want > 0", c.Rounds)
	}
	if c.CaptureShare <= 0 || c.CaptureShare > 1 {
		t.Fatalf("CaptureShare = %v, want in (0,1]", c.CaptureShare)
	}
	if c.Cost.OutputWeight < c.Cost.InputWeight {
		t.Fatalf("cost schedule should weight output >= input")
	}
	if c.Vuln == "" {
		t.Fatalf("default vuln must be set")
	}
}

func TestPresetUnknownReturnsFalse(t *testing.T) {
	if _, ok := Preset("does-not-exist"); ok {
		t.Fatal("unknown preset returned ok=true")
	}
}

func TestPresetScarcityIsLeanerThanMarathon(t *testing.T) {
	scarcity, ok1 := Preset("scarcity")
	marathon, ok2 := Preset("marathon")
	if !ok1 || !ok2 {
		t.Fatalf("presets missing: scarcity=%v marathon=%v", ok1, ok2)
	}
	if scarcity.StartTokens >= marathon.StartTokens {
		t.Fatalf("scarcity (%d) should start leaner than marathon (%d)", scarcity.StartTokens, marathon.StartTokens)
	}
	if scarcity.Economy != "scarcity" {
		t.Fatalf("preset should stamp its name into Economy, got %q", scarcity.Economy)
	}
}

func TestPresetBlitzForcesAggressionViaDecay(t *testing.T) {
	blitz, ok := Preset("blitz")
	if !ok {
		t.Fatal("blitz preset missing")
	}
	if blitz.DecayPerRound <= 0 {
		t.Fatalf("blitz should tax idle balance each round, DecayPerRound = %v", blitz.DecayPerRound)
	}
}
