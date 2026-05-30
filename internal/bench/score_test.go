package bench

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestScore_CleanSuccess(t *testing.T) {
	b := Budget{MaxTurns: 10}
	m := RunMetrics{Turns: 5, ToolCalls: 4, ErroredCalls: 0, StoppedClean: true}
	d, total := Score(b, m, true, DefaultWeights)

	if !approx(d.Success, 1) {
		t.Errorf("success = %v, want 1", d.Success)
	}
	if !approx(d.Format, 1) {
		t.Errorf("format = %v, want 1", d.Format)
	}
	if !approx(d.Efficiency, 0.5) {
		t.Errorf("efficiency = %v, want 0.5 (5/10 turns)", d.Efficiency)
	}
	// 100*(0.6*1 + 0.25*1 + 0.15*0.5) = 92.5
	if !approx(total, 92.5) {
		t.Errorf("total = %v, want 92.5", total)
	}
}

func TestScore_FailedNoCleanStop(t *testing.T) {
	b := Budget{MaxTurns: 10}
	m := RunMetrics{Turns: 10, ToolCalls: 4, ErroredCalls: 2, StoppedClean: false}
	d, total := Score(b, m, false, DefaultWeights)

	if d.Success != 0 {
		t.Errorf("success = %v, want 0", d.Success)
	}
	// format: 1 - 2/4 = 0.5, minus 0.2 clean-stop penalty = 0.3
	if !approx(d.Format, 0.3) {
		t.Errorf("format = %v, want 0.3", d.Format)
	}
	// efficiency: 1 - 10/10 = 0
	if !approx(d.Efficiency, 0) {
		t.Errorf("efficiency = %v, want 0", d.Efficiency)
	}
	// 100*(0 + 0.25*0.3 + 0) = 7.5
	if !approx(total, 7.5) {
		t.Errorf("total = %v, want 7.5", total)
	}
}

func TestFormatScore_FloorsAtZero(t *testing.T) {
	m := RunMetrics{ToolCalls: 4, ErroredCalls: 4, StoppedClean: false}
	if got := formatScore(m); got != 0 {
		t.Errorf("format = %v, want 0 (floored)", got)
	}
}

func TestEfficiency_NoBudgetIsFull(t *testing.T) {
	if got := efficiencyScore(Budget{}, RunMetrics{Turns: 99}); got != 1 {
		t.Errorf("no-budget efficiency = %v, want 1", got)
	}
}

func TestEfficiency_TurnHeadroom(t *testing.T) {
	// turns headroom 1-2/10 = 0.8
	if got := efficiencyScore(Budget{MaxTurns: 10}, RunMetrics{Turns: 2}); !approx(got, 0.8) {
		t.Errorf("efficiency = %v, want 0.8", got)
	}
}
