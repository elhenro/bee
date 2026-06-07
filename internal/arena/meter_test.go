package arena

import "testing"

func TestMeterUsageWeightsInputAndOutput(t *testing.T) {
	s := CostSchedule{InputWeight: 1.0, OutputWeight: 3.0}
	got := s.MeterUsage(100, 10)
	if got != 130 {
		t.Fatalf("MeterUsage(100,10) = %d, want 130", got)
	}
}

func TestMeterUsageRoundsHalfAway(t *testing.T) {
	s := CostSchedule{InputWeight: 0.5, OutputWeight: 0.5}
	if got := s.MeterUsage(1, 0); got != 1 { // 0.5 rounds up to 1
		t.Fatalf("MeterUsage(1,0) = %d, want 1", got)
	}
	if got := s.MeterUsage(2, 0); got != 1 { // 1.0 stays 1
		t.Fatalf("MeterUsage(2,0) = %d, want 1", got)
	}
}

func TestMeterToolSurchargeDefaultsZero(t *testing.T) {
	s := CostSchedule{ToolSurcharge: map[string]int{"http_probe": 1800, "bash": 400}}
	if got := s.MeterTool("http_probe"); got != 1800 {
		t.Fatalf("MeterTool(http_probe) = %d, want 1800", got)
	}
	if got := s.MeterTool("read"); got != 0 {
		t.Fatalf("MeterTool(read) = %d, want 0 (unlisted)", got)
	}
}

func TestDefaultCostScheduleOutputCostsMore(t *testing.T) {
	s := DefaultCostSchedule()
	if s.InputWeight <= 0 || s.OutputWeight <= 0 {
		t.Fatalf("default weights must be positive: in=%v out=%v", s.InputWeight, s.OutputWeight)
	}
	if s.OutputWeight < s.InputWeight {
		t.Fatalf("output should cost >= input: in=%v out=%v", s.InputWeight, s.OutputWeight)
	}
}
