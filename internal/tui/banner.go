// Package tui implements bee's interactive Bubbletea interface.
//
// Startup banner art — minimal sigils, no emoji.
package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// BannerVariant selects which startup banner art to render. random picks
// one per launch so a new shell feels alive without committing to one aesthetic.
type BannerVariant int

const (
	BannerHex    BannerVariant = iota // diamond sigil (default)
	BannerSwarm                       // small constellation
	BannerComb                        // honeycomb sliver
	BannerFlower                      // radial sigil
)

// ParseBannerVariant maps BEE_BANNER values to a variant. "random" / "" /
// unknown returns a pseudo-random pick seeded from the clock.
func ParseBannerVariant(s string) BannerVariant {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "hex", "hexagon":
		return BannerHex
	case "swarm":
		return BannerSwarm
	case "comb", "honeycomb":
		return BannerComb
	case "flower":
		return BannerFlower
	default:
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		return BannerVariant(r.Intn(4))
	}
}

const bannerArt = `
  ⬡
 ⬡ ⬢
  ⬡
`

const bannerSwarmArt = `
 ⬡   ⬡
   ⬢
`

const bannerCombArt = `
 ⬡ ⬢ ⬡ ⬢ ⬡
`

const bannerFlowerArt = `
  · ⬡ ·
  ⬢   ⬢
  · ⬡ ·
`

func bannerFor(v BannerVariant) string {
	switch v {
	case BannerSwarm:
		return bannerSwarmArt
	case BannerComb:
		return bannerCombArt
	case BannerFlower:
		return bannerFlowerArt
	default:
		return bannerArt
	}
}

// RenderBanner legacy entry — delegates to hex variant. model arg accepted
// for api compatibility but ignored; the live top status bar already shows
// `provider/model  cwd`, so the splash stays trim.
func RenderBanner(version, model string) string {
	_ = model
	return RenderBannerVariant(BannerHex, version, "")
}

// RenderBannerVariant returns a styled startup banner. version optional.
// model arg accepted for api compatibility but no longer rendered — model
// identity already lives on the persistent top status bar, so the splash
// stays minimal (sigil + "bee <version>").
func RenderBannerVariant(v BannerVariant, version, model string) string {
	_ = model
	honey := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	smoke := lipgloss.NewStyle().Foreground(fgSmoke)

	var b strings.Builder
	b.WriteString(honey.Render(bannerFor(v)))
	b.WriteString("\n")

	info := "bee"
	if version != "" {
		info += " " + version
	}
	b.WriteString(smoke.Render(info))
	b.WriteString("\n")
	return b.String()
}

// RenderBannerCompact returns a single-line banner for headless mode. model
// arg accepted for api compatibility but not rendered (same rationale as the
// full banner — status bar already carries it).
func RenderBannerCompact(version, model string) string {
	_ = model
	honey := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	dim := lipgloss.NewStyle().Foreground(fgOyster)

	parts := []string{honey.Render("⬢")}
	if version != "" {
		parts = append(parts, dim.Render("bee "+version))
	}
	return strings.Join(parts, "  ") + "\n"
}

// bannerASCII pure-ASCII fallback for terminals without unicode glyph support.
func bannerASCII() string {
	return fmt.Sprintf(`
   .
  ( )
   .
  bee
`)
}
