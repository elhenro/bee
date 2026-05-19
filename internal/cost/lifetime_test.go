package cost

import (
	"path/filepath"
	"testing"
)

func TestLifetime_PersistsAcrossLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lifetime_tokens.json")
	t.Setenv("BEE_LIFETIME_TOKENS", path)
	ResetLifetimeForTest()

	AddLifetime(1000, 200)
	AddLifetime(500, 50)

	in, out := LifetimeTotals()
	if in != 1500 || out != 250 {
		t.Fatalf("first session totals = (%d,%d); want (1500,250)", in, out)
	}

	// Simulate a fresh bee start: drop cache, re-read from disk.
	ResetLifetimeForTest()
	t.Setenv("BEE_LIFETIME_TOKENS", path)
	in, out = LifetimeTotals()
	if in != 1500 || out != 250 {
		t.Fatalf("reloaded totals = (%d,%d); want (1500,250)", in, out)
	}

	AddLifetime(1, 1)
	in, out = LifetimeTotals()
	if in != 1501 || out != 251 {
		t.Fatalf("after second-session bump = (%d,%d); want (1501,251)", in, out)
	}
}

func TestLifetime_IgnoresZeroAndNegative(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_LIFETIME_TOKENS", filepath.Join(dir, "lt.json"))
	ResetLifetimeForTest()

	AddLifetime(0, 0)
	AddLifetime(-5, -5)
	in, out := LifetimeTotals()
	if in != 0 || out != 0 {
		t.Fatalf("zero/negative leaked into totals: (%d,%d)", in, out)
	}
}

func TestFormatLifetimeTokens(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1_000, "1K"},
		{1_500, "1.5K"},
		{12_300, "12K"},
		{999_000, "999K"},
		{1_000_000, "1M"},
		{1_234_000, "1.2M"},
		{12_300_000, "12M"},
		{2_500_000_000, "2.5B"},
	}
	for _, c := range cases {
		if got := FormatLifetimeTokens(c.in); got != c.want {
			t.Errorf("FormatLifetimeTokens(%d) = %q; want %q", c.in, got, c.want)
		}
	}
}
