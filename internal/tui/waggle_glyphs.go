package tui

import "strings"

// waggle-dance glyph alphabet. honeybees advertise a goal with a dance:
// round = search, waggle-run = go, turn-back = return. the busy row and
// pending tool rows read their state out of the same alphabet, so the
// pattern — not a spinner glyph — carries the signal.

// waggleCycle is the frame period of one full dance.
const waggleCycle = 12

// dance-form anchors inside wagglePose: the busy row snaps to the round
// dance while tokens flow and to the straight waggle-run once the stream
// has been quiet for stallDanceFrames ticks. form carries the signal.
const (
	waggleRunStart   = 3 // frames 3-8: straight run down the cell
	stallDanceFrames = 3 // idle ticks before the dance switches form
)

// wagglePose[f] = body x,y + wing x,y inside one 2×4 braille cell.
// hand-tuned: frames 0-2 loop a round dance, 3-8 run the waggle straight
// down the cell, 9-11 sweep the turn-back. all 12 tuples distinct so the
// glyph visibly changes every frame.
var wagglePose = [waggleCycle][4]int{
	{0, 1, 1, 0},
	{1, 1, 0, 0},
	{0, 2, 1, 3},
	{0, 1, 0, 0},
	{0, 2, 0, 1},
	{1, 2, 1, 1},
	{0, 3, 0, 2},
	{1, 3, 1, 2},
	{0, 3, 1, 3},
	{1, 2, 0, 3},
	{0, 2, 1, 2},
	{1, 1, 0, 2},
}

// waggleGlyph renders one frame of the dance as a single braille cell:
// body + wing, with the previous frame's body as a motion trail.
func waggleGlyph(frame int) string {
	f := ((frame % waggleCycle) + waggleCycle) % waggleCycle
	c := NewDrawilleCanvas(braillePxW, braillePxH)
	p, prev := wagglePose[f], wagglePose[(f+waggleCycle-1)%waggleCycle]
	c.SetPixel(prev[0], prev[1], true) // trail
	c.SetPixel(p[0], p[1], true)       // body
	c.SetPixel(p[2], p[3], true)       // wing
	return c.ToBraille()
}

// waggleStrip renders a cells-wide mini-dance, one bee per cell. offset
// staggers the phases so a row of them reads as a swarm mid-communication
// instead of a lockstep flipbook.
func waggleStrip(frame, cells, offset int) string {
	if cells < 1 {
		cells = 1
	}
	if cells > 4 {
		cells = 4
	}
	var b strings.Builder
	for i := 0; i < cells; i++ {
		b.WriteString(waggleGlyph(frame - i*2 - offset*3))
	}
	return b.String()
}

// wagglePendingBullet renders the in-flight tool bullet: a 2-cell dance
// strip whose phase is derived from the call id so parallel calls each
// hold their own beat. Settles into the static outcome hex the moment
// the call resolves (see stream_toolcard bullet()).
func wagglePendingBullet(id string, frame int) string {
	seed := 0
	for _, r := range id {
		seed = seed*31 + int(r)
	}
	if seed < 0 {
		seed = -seed
	}
	return waggleStrip(frame, 2, seed%waggleCycle)
}
