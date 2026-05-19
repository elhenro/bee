package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/cost"
)

func TestParseBannerVariant_NamedValues(t *testing.T) {
	cases := map[string]BannerVariant{
		"hex":       BannerHex,
		"hexagon":   BannerHex,
		"swarm":     BannerSwarm,
		"comb":      BannerComb,
		"honeycomb": BannerComb,
		"flower":    BannerFlower,
	}
	for in, want := range cases {
		if got := ParseBannerVariant(in); got != want {
			t.Errorf("ParseBannerVariant(%q) = %v want %v", in, got, want)
		}
	}
}

func TestParseBannerVariant_RandomInRange(t *testing.T) {
	// "random" / "" / unknown all return some valid variant — just assert
	// the bound. Non-determinism is the whole point so we don't pin output.
	for _, in := range []string{"", "random", "????"} {
		v := ParseBannerVariant(in)
		if v < BannerHex || v > BannerFlower {
			t.Errorf("ParseBannerVariant(%q) out of range: %v", in, v)
		}
	}
}

func TestRenderBannerVariant_AllVariantsRender(t *testing.T) {
	// every variant must produce non-empty output containing "bee" trim.
	// model name is intentionally absent — the persistent top status bar
	// already shows provider/model, so the splash banner stays trim.
	const model = "claude-opus-4-7"
	for _, v := range []BannerVariant{BannerHex, BannerSwarm, BannerComb, BannerFlower} {
		out := stripANSI(RenderBannerVariant(v, "v0.1", model))
		if !strings.Contains(out, "bee") {
			t.Errorf("variant %v missing 'bee' trim: %q", v, out)
		}
		if strings.Contains(out, model) {
			t.Errorf("variant %v must not show model in startup banner: %q", v, out)
		}
	}
}

func TestRenderBannerVariant_CompactBannerOmitsModel(t *testing.T) {
	const model = "qwen3-coder-30b"
	out := stripANSI(RenderBannerCompact("v0.1", model))
	if !strings.Contains(out, "bee v0.1") {
		t.Errorf("compact banner missing version: %q", out)
	}
	if strings.Contains(out, model) {
		t.Errorf("compact banner must not include model: %q", out)
	}
}

func TestRenderBanner_LegacyDelegatesToHex(t *testing.T) {
	legacy := stripANSI(RenderBanner("v0.1", "m"))
	hex := stripANSI(RenderBannerVariant(BannerHex, "v0.1", "m"))
	if legacy != hex {
		t.Errorf("RenderBanner not equal to RenderBannerVariant(BannerHex):\nlegacy=%q\nhex=%q", legacy, hex)
	}
}

func TestRenderBanner_LifetimeTokensTrailer(t *testing.T) {
	// Pin a fresh totals file so persisted state on the dev box doesn't
	// leak into the assertion (and so the test doesn't pollute it).
	dir := t.TempDir()
	t.Setenv("BEE_LIFETIME_TOKENS", filepath.Join(dir, "lt.json"))
	cost.ResetLifetimeForTest()

	out := stripANSI(RenderBannerVariant(BannerHex, "v0.1", ""))
	if strings.Contains(out, "tok") {
		t.Errorf("fresh install banner should omit token trailer: %q", out)
	}

	cost.AddLifetime(1_200_000, 300_000)
	out = stripANSI(RenderBannerVariant(BannerHex, "v0.1", ""))
	if !strings.Contains(out, "1.5M tok") {
		t.Errorf("banner missing lifetime trailer: %q", out)
	}

	compact := stripANSI(RenderBannerCompact("v0.1", ""))
	if !strings.Contains(compact, "1.5M tok") {
		t.Errorf("compact banner missing lifetime trailer: %q", compact)
	}
}
