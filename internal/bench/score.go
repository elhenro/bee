package bench

// Weights blend the three dimensions into a single 0-100 score. success
// dominates: a model that thrashes but never works should score low regardless.
type Weights struct {
	Success    float64
	Format     float64
	Efficiency float64
}

// DefaultWeights — success matters most, format is the next most common
// small-model failure, efficiency is a tiebreaker.
var DefaultWeights = Weights{Success: 0.6, Format: 0.25, Efficiency: 0.15}

// Dims holds the three normalized 0-1 dimension scores.
type Dims struct {
	Success    float64 `json:"success"`
	Format     float64 `json:"format"`
	Efficiency float64 `json:"efficiency"`
}

// Score computes the per-dimension scores and the weighted total (0-100).
//
//	success    — 1.0 if the task demonstrably worked, else 0.0 (binary).
//	format     — fraction of tool calls that did not error, with a clean-stop
//	             penalty when the loop hit a cap instead of finishing.
//	efficiency — how far under budget the run stayed (turns + tokens averaged).
func Score(b Budget, m RunMetrics, succeeded bool, tokens int, w Weights) (Dims, float64) {
	var d Dims

	if succeeded {
		d.Success = 1
	}

	d.Format = formatScore(m)
	d.Efficiency = efficiencyScore(b, m, tokens)

	total := 100 * (w.Success*d.Success + w.Format*d.Format + w.Efficiency*d.Efficiency)
	return d, total
}

// formatScore rewards clean tool use. Errored calls (malformed input, unknown
// tool, failed exec) drag it down; never finishing cleanly costs a flat 0.2.
func formatScore(m RunMetrics) float64 {
	s := 1.0
	if m.ToolCalls > 0 {
		s = 1 - float64(m.ErroredCalls)/float64(m.ToolCalls)
	}
	if !m.StoppedClean {
		s -= 0.2
	}
	return clamp01(s)
}

// efficiencyScore averages the headroom left on each budget dimension. A run
// that used half its turn budget scores 0.5 on turns. Dimensions with a
// zero/absent budget are skipped so they don't distort the average.
func efficiencyScore(b Budget, m RunMetrics, tokens int) float64 {
	var sum float64
	var n int
	if b.MaxTurns > 0 {
		sum += clamp01(1 - float64(m.Turns)/float64(b.MaxTurns))
		n++
	}
	if b.MaxTokens > 0 && tokens > 0 {
		sum += clamp01(1 - float64(tokens)/float64(b.MaxTokens))
		n++
	}
	if n == 0 {
		return 1
	}
	return sum / float64(n)
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
