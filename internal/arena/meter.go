package arena

import "math"

// CostSchedule prices a combatant's actions in nectar. Model tokens are the
// primary metabolic cost (the proxy meters every turn's usage); tool calls and
// inter-agent messages add surcharges so acting costs stamina, not just
// thinking. All knobs are config-driven (see config.go / the [wars] block).
type CostSchedule struct {
	InputWeight   float64        // nectar per model input token
	OutputWeight  float64        // nectar per model output token (>= input)
	ToolSurcharge map[string]int // per tool-call surcharge by Spec().Name
	MessageCost   int            // per inter-agent message
}

// MeterUsage returns the nectar cost of one model turn's token usage, rounded
// to the nearest whole nectar (half away from zero).
func (s CostSchedule) MeterUsage(input, output int) int {
	n := s.InputWeight*float64(input) + s.OutputWeight*float64(output)
	return int(math.Round(n))
}

// MeterTool returns the surcharge for a tool call by name, 0 when unlisted.
func (s CostSchedule) MeterTool(name string) int {
	if s.ToolSurcharge == nil {
		return 0
	}
	return s.ToolSurcharge[name]
}

// DefaultCostSchedule is the "normal" economy: 1 nectar per input token, output
// weighted heavier (mirrors real provider pricing where output costs more), and
// surcharges that make network probing and cross-agent chatter expensive.
func DefaultCostSchedule() CostSchedule {
	return CostSchedule{
		InputWeight:  1.0,
		OutputWeight: 5.0,
		MessageCost:  2500,
		ToolSurcharge: map[string]int{
			"bash":       400,
			"http_probe": 1800,
			"web_fetch":  1800,
			"web_search": 1800,
			"write":      900,
			"edit_diff":  900,
		},
	}
}
