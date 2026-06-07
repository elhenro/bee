package arena

// Config holds the tunable "gears" of one bee-wars match. It is self-contained
// (not wired into the central config.Config) so a match is parameterized purely
// by CLI flags + an optional wars preset; the central config is read only for
// provider/model resolution.
type Config struct {
	StartTokens   int          // starting nectar per side (life + bankroll)
	Rounds        int          // turn-round cap before a decision is forced
	Cost          CostSchedule // per-action nectar pricing
	AntePerRound  int          // nectar each side antes into the round pot
	CaptureShare  float64      // fraction of loser's balance taken on capture (1.0 = winner-take-all)
	RegenPerRound int          // nectar income per round (UBI; dampens snowballing)
	DecayPerRound float64      // fraction of balance taxed per round (anti-turtle)
	Vuln          string       // defender vuln module: "cmdi" | "traversal" | ...
	Difficulty    int          // 0 = naive; higher adds filters the attacker must bypass
	Handicap      bool         // grant the weaker-tier/lower-ELO side nectar odds
	USDFuse       float64      // per-match real-dollar cap across both sides; 0 = unlimited
	Economy       string       // preset name, recorded in the ledger

	// Sealed makes the combat network fully airtight (docker --internal: no
	// gateway, no egress) and enforces the no-egress assertion. Model access
	// must then come from a sibling model container on the same net. Default
	// false keeps v1 runnable on macOS where combatants reach the host model
	// proxy via host.docker.internal.
	Sealed bool
}

// DefaultConfig is the "normal" economy: a moderate budget, winner-take-all
// capture, no regen/decay, the command-injection vuln.
func DefaultConfig() Config {
	return Config{
		StartTokens:  200_000,
		Rounds:       12,
		Cost:         DefaultCostSchedule(),
		AntePerRound: 0,
		CaptureShare: 1.0,
		Vuln:         "cmdi",
		Difficulty:   0,
		Handicap:     true,
		Economy:      "normal",
	}
}

// Preset returns a named economy preset. The three shipped presets shift the
// match's character: scarcity is bankruptcy-dominant and surgical, blitz forces
// fast aggression via decay, marathon is a long survival siege.
func Preset(name string) (Config, bool) {
	c := DefaultConfig()
	switch name {
	case "scarcity":
		c.Economy = "scarcity"
		c.StartTokens = 120_000
		c.Rounds = 30
		c.Cost.MessageCost = 6000
		c.Cost.OutputWeight = 8.0
		c.AntePerRound = 20_000
		return c, true
	case "blitz":
		c.Economy = "blitz"
		c.StartTokens = 150_000
		c.Rounds = 8
		c.DecayPerRound = 0.08
		c.Cost.MessageCost = 2000
		c.AntePerRound = 15_000
		return c, true
	case "marathon":
		c.Economy = "marathon"
		c.StartTokens = 5_000_000
		c.Rounds = 60
		c.RegenPerRound = 50_000
		c.CaptureShare = 0.6
		return c, true
	default:
		return Config{}, false
	}
}
