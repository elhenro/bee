// Package tui implements bee's interactive Bubbletea interface.
//
// Startup intro animation — short braille frame sequence rendered above the
// input bar by the bubbletea model so typing stays available throughout.

package tui

import (
	"time"
)

// IntroFrame is one rendered frame of the startup animation.
type IntroFrame struct {
	Text     string
	Subtitle string
}

// IntroStyle controls which animation plays on startup.
type IntroStyle int

const (
	IntroStyleRandom    IntroStyle = iota // pick one of the concrete styles per launch
	IntroStyleLifecycle                   // egg → larva → pupa → flight
	IntroStyleSwarm                       // swarm of dots
	IntroStyleHex                         // hex outline orbit
	IntroStyleDance                       // waggle dance figure-8
	IntroStyleDrip                        // honey drop + ripple
	IntroStyleBloom                       // pollen converge → hex frame + bee → pulse rings
	IntroStyleConstell                    // constellation: dots twinkle into hex tessellation
)

// IntroStyleDefault is the value used when BEE_BANNER is unset.
const IntroStyleDefault = IntroStyleBloom

// concreteIntroStyles is the pool used by random. Bloom + Constellation are
// weighted by listing them more — they are the new flagship designs.
var concreteIntroStyles = []IntroStyle{
	IntroStyleBloom,
	IntroStyleBloom,
	IntroStyleConstell,
	IntroStyleLifecycle,
	IntroStyleSwarm,
	IntroStyleHex,
	IntroStyleDance,
	IntroStyleDrip,
}

// ParseIntroStyle maps BEE_BANNER values to a style.
func ParseIntroStyle(s string) IntroStyle {
	switch s {
	case "", "random", "rand":
		return IntroStyleRandom
	case "default", "life", "lifecycle":
		return IntroStyleLifecycle
	case "swarm":
		return IntroStyleSwarm
	case "hex":
		return IntroStyleHex
	case "dance", "waggle":
		return IntroStyleDance
	case "drip", "honey":
		return IntroStyleDrip
	case "bloom", "pollen":
		return IntroStyleBloom
	case "constell", "constellation", "stars":
		return IntroStyleConstell
	default:
		return IntroStyleRandom
	}
}

// pickStyle resolves Random to a concrete style per launch (rand seeded by time).
func pickStyle(s IntroStyle) IntroStyle {
	if s != IntroStyleRandom {
		return s
	}
	return concreteIntroStyles[time.Now().UnixNano()%int64(len(concreteIntroStyles))]
}

// introArtRows is the fixed number of braille text rows per frame. 5 rows
// (20 px vertical) gives bloom/constell room for a proper hex without
// degenerating into a flat horizontal line on wide terminals.
const introArtRows = 5

// introFrameDelay is the per-frame sleep. Keep total animation short.
const introFrameDelay = 70 * time.Millisecond

func introFrames(style IntroStyle, width int) []IntroFrame {
	cells := clampCells(width)
	cw := cells * braillePxW
	h := introArtRows * braillePxH

	switch pickStyle(style) {
	case IntroStyleSwarm:
		return swarmFrames(cw, h, 24)
	case IntroStyleHex:
		return hexFrames(cw, h, 24)
	case IntroStyleDance:
		return danceFrames(cw, h, 30)
	case IntroStyleDrip:
		return dripFrames(cw, h, 28)
	case IntroStyleLifecycle:
		return lifecycleFrames(cw, h)
	case IntroStyleConstell:
		return constellFrames(cw, h, 32)
	case IntroStyleBloom:
		return bloomFrames(cw, h, 34)
	default:
		return bloomFrames(cw, h, 34)
	}
}
