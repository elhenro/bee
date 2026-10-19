package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/cost"
)

func TestIntroBannerLifetimeTokensTrailer(t *testing.T) {
	// pin a fresh totals file so persisted state on the dev box doesn't leak in
	t.Setenv("BEE_LIFETIME_TOKENS", filepath.Join(t.TempDir(), "lt.json"))
	cost.ResetLifetimeForTest()

	if got := lifetimeTokensTrailer(); got != "" {
		t.Errorf("fresh install should omit the trailer, got %q", got)
	}
	if strings.Contains(stripANSI(buildIntroBannerLine(80, false)), "tok") {
		t.Error("fresh install banner line shows a token trailer")
	}

	cost.AddLifetime(1_200_000, 300_000)
	if got := lifetimeTokensTrailer(); !strings.Contains(got, "1.5M tok") {
		t.Errorf("trailer = %q, want 1.5M tok", got)
	}
	if !strings.Contains(stripANSI(buildIntroBannerLine(80, false)), "1.5M tok") {
		t.Error("banner line missing lifetime trailer")
	}
}
