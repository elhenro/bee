package tui

import "time"

// rateSample is one (time, chars) entry in the sliding tok/s window.
// Pushed only when the tick produced output, so the window contains
// only active-generation data.
type rateSample struct {
	at    time.Time
	chars int
}

// loaderState is the busy-row animation and turn clock state, embedded in Model.
type loaderState struct {
	// loaderFrame drives the pre-token streaming animation. Incremented by
	// loaderTickMsg while state == StateStreaming and partial is empty.
	loaderFrame int

	// turnOutChars is the cumulative chars (text + reasoning) generated this
	// turn — the live "output" figure for the token-stream loader. Reset on
	// submit, bumped per delta. Chars not tokens: the real output-token tally
	// lands at turn end via the cost flash, so the strip shows honest live
	// throughput without fabricating token math.
	turnOutChars int
	// turnStartOutput snapshots the cost tracker's cumulative output tokens at
	// submit. In queen/hive mode the work runs on sub-engines, so no main-loop
	// deltas bump turnOutChars and the live "↓" would read 0; the cost-tracker
	// delta since this baseline gives the real output figure instead.
	turnStartOutput int
	// loaderSeed varies the procedural particle layout per turn. Rolled on
	// submit so each generation looks distinct.
	loaderSeed int64
	// loaderStallFrames counts consecutive loader ticks with no output
	// growth; reset on any growth. Drives the busy-row dance form: the bee
	// loops the round dance while tokens flow and switches to the straight
	// waggle-run when the stream stalls, so the pattern reports stream
	// health without a single word of text.
	loaderStallFrames int
	// loaderRate is chars produced since the previous loaderTick — the raw
	// per-tick delta, kept for the tok/s window and for the existing tests.
	// loaderSampleChars holds the prior sample point. loaderRateEMA is the
	// smoothed version actually fed to the braille loader as particle density;
	// it caps the 1-tick flash when a provider batch lands in a single tick
	// (and when hasThinkingBlock resets the rate between reasoning and answer).
	loaderRate        int
	loaderRateEMA     float64
	loaderSampleChars int
	// loaderRateSamples is a sliding 10s window of (time, chars) used to
	// compute the displayed tok/s. Idle ticks push nothing, so the window
	// naturally excludes gaps. loaderRateTokS is the cached result the
	// renderer reads; recomputed from the window on every tick.
	loaderRateSamples []rateSample
	loaderRateTokS    float64

	// turnStartedAt is wall-clock when the current turn left submit(). Zero
	// when no turn in flight. Top-bar timer reads time.Since on every tick
	// so the elapsed string updates live without per-second state.
	turnStartedAt time.Time
	// lastTurnDuration is how long the most recent turn took end-to-end.
	// Set on every turnDoneMsg path (success, cancel, error). Persists in
	// the top bar after streaming ends until the next submit clears it.
	lastTurnDuration time.Duration

	// loader is the configured busy-indicator name (tui.loader), re-applied
	// whenever the stream renderer is rebuilt on resize
	loader string

	// showLoader gates the streaming "generating" animation live. Toggle
	// via /settings; persists across launches.
	showLoader bool

	// loader readout figure toggles: ↑ input, ↓ output, avg tok/s. Default
	// true; persisted via /settings. Fed into LoaderStats each frame.
	showLoaderIn   bool
	showLoaderOut  bool
	showLoaderRate bool
}
