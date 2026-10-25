// Package tui implements bee's interactive Bubbletea interface.
//
// Startup intro animation — short braille frame sequence rendered above the
// input bar by the bubbletea model so typing stays available throughout.

package tui

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// Version is the bee build version surfaced in the intro placeholder after
// the startup animation finishes. cmd/bee sets it at process start so the
// linker-injected build tag flows through. Default kept in sync with main.
var Version = "0.1.0"

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
	IntroStyleRain                        // honey-drop rain at staggered columns
	IntroStyleSpiral                      // single particle traces inward spiral with trail
	IntroStyleWave                        // sine wave sweep with crest cluster
	IntroStyleOrbit                       // three particles orbit a center at different radii
)

// concreteIntroStyles is the pool used by random — uniform weight so launches
// rotate evenly across all variants.
var concreteIntroStyles = []IntroStyle{
	IntroStyleBloom,
	IntroStyleConstell,
	IntroStyleLifecycle,
	IntroStyleSwarm,
	IntroStyleHex,
	IntroStyleDance,
	IntroStyleDrip,
	IntroStyleRain,
	IntroStyleSpiral,
	IntroStyleWave,
	IntroStyleOrbit,
}

var introNames = map[string]IntroStyle{
	"default": IntroStyleLifecycle, "life": IntroStyleLifecycle, "lifecycle": IntroStyleLifecycle,
	"swarm": IntroStyleSwarm,
	"hex":   IntroStyleHex,
	"dance": IntroStyleDance, "waggle": IntroStyleDance,
	"drip": IntroStyleDrip, "honey": IntroStyleDrip,
	"bloom": IntroStyleBloom, "pollen": IntroStyleBloom,
	"constell": IntroStyleConstell, "constellation": IntroStyleConstell, "stars": IntroStyleConstell,
	"rain":   IntroStyleRain,
	"spiral": IntroStyleSpiral,
	"wave":   IntroStyleWave,
	"orbit":  IntroStyleOrbit,
}

// ParseIntroStyle maps BEE_BANNER values to a style. Empty, "random" and
// unknown names pick a random one.
func ParseIntroStyle(s string) IntroStyle { return introNames[s] }

// pickStyle resolves Random to a concrete style per launch. Uses crypto/rand
// for true entropy — time-based modulo can repeat on quick relaunches.
func pickStyle(s IntroStyle) IntroStyle {
	if s != IntroStyleRandom {
		return s
	}
	n := len(concreteIntroStyles)
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return concreteIntroStyles[time.Now().UnixNano()%int64(n)]
	}
	return concreteIntroStyles[binary.LittleEndian.Uint64(b[:])%uint64(n)]
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
	case IntroStyleRain:
		return rainFrames(cw, h, 30)
	case IntroStyleSpiral:
		return spiralFrames(cw, h, 32)
	case IntroStyleWave:
		return waveFrames(cw, h, 30)
	case IntroStyleOrbit:
		return orbitFrames(cw, h, 32)
	default:
		return bloomFrames(cw, h, 34)
	}
}
