package cost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageAppendReadAggregate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_USAGE_LOG", filepath.Join(dir, "usage.jsonl"))
	ResetUsageForTest()

	now := time.Now().UTC()
	recs := []UsageRecord{
		{Time: now.Add(-1 * time.Hour), Provider: "openrouter", Model: "m1", Input: 100, Output: 50, USD: 0.01, CostReported: true},
		{Time: now.Add(-2 * time.Hour), Provider: "openrouter", Model: "m2", Input: 200, Output: 100, USD: 0.02},
		{Time: now.Add(-3 * 24 * time.Hour), Provider: "openai", Model: "m3", Input: 300, Output: 150, USD: 0.05},
		{Time: now.Add(-10 * 24 * time.Hour), Provider: "ollama", Model: "m4", Input: 400, Output: 200},
		{Time: now.Add(-40 * 24 * time.Hour), Provider: "openai", Model: "m5", Input: 500, Output: 250, USD: 0.10},
	}
	for _, r := range recs {
		AppendUsage(r)
	}

	got, err := ReadUsage()
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 records, got %d", len(got))
	}

	day := Aggregate(got, 24*time.Hour, now, 24)
	if day.Total.Calls != 2 || day.Total.Input != 300 || day.Total.Output != 150 {
		t.Errorf("day total = %+v", day.Total)
	}
	if !day.Estimated {
		t.Error("day should be estimated: m2 cost is not provider-reported")
	}
	if day.ByProvider["openrouter"].Calls != 2 {
		t.Errorf("day openrouter calls = %d, want 2", day.ByProvider["openrouter"].Calls)
	}
	if len(day.Series) != 24 {
		t.Errorf("day series len = %d, want 24", len(day.Series))
	}

	if c := Aggregate(got, 7*24*time.Hour, now, 7).Total.Calls; c != 3 {
		t.Errorf("week calls = %d, want 3", c)
	}
	if c := Aggregate(got, 30*24*time.Hour, now, 30).Total.Calls; c != 4 {
		t.Errorf("month calls = %d, want 4", c)
	}
	all := Aggregate(got, 0, now, 24)
	if all.Total.Calls != 5 {
		t.Errorf("all calls = %d, want 5", all.Total.Calls)
	}
	if all.ByModel["m5"].Input != 500 {
		t.Errorf("all m5 input = %d, want 500", all.ByModel["m5"].Input)
	}
}

func TestReadUsageSkipsPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")
	t.Setenv("BEE_USAGE_LOG", path)
	ResetUsageForTest()

	body := `{"t":"2026-06-01T00:00:00Z","provider":"openrouter","model":"m1","in":10,"out":5}` + "\n" +
		`{"t":"2026-06-01T01:00:00Z","provid` // truncated trailing line (simulated crash)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadUsage()
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 parseable record, got %d", len(got))
	}
}

func TestReadUsageMissingFile(t *testing.T) {
	t.Setenv("BEE_USAGE_LOG", filepath.Join(t.TempDir(), "none.jsonl"))
	ResetUsageForTest()
	got, err := ReadUsage()
	if err != nil || got != nil {
		t.Fatalf("missing file should be (nil,nil), got (%v,%v)", got, err)
	}
}

func TestAppendUsageSkipsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	t.Setenv("BEE_USAGE_LOG", path)
	ResetUsageForTest()
	AppendUsage(UsageRecord{Provider: "x", Model: "y"}) // zero tokens
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("zero-token record should not create the log")
	}
}
