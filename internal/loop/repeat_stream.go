package loop

// streaming degenerate-repetition detection. Small / local models sometimes
// fall into a token loop, emitting the same phrase or cycle of lines over and
// over until they hit max_tokens. The provider never closes the stream, so the
// turn loop can't make progress and the UI looks frozen. We watch the growing
// buffer and cut the stream once the tail is unmistakably periodic.
const (
	// loopScanWindow caps how many trailing bytes the detector inspects, so the
	// check stays cheap even on a long but legitimate response.
	loopScanWindow = 8192
	// loopScanStride runs the detector once per this many new output bytes
	// instead of on every delta — amortizes the cost to near zero.
	loopScanStride = 512
	// loopMinReps: the tail must repeat a unit at least this many times before
	// we call it degenerate. high enough that legit repetition (short lists,
	// table rules) doesn't trip it.
	loopMinReps = 6
	// loopMaxPeriod caps the repeating-unit length we look for — covers a few
	// lines of looped prose without scanning the whole window per candidate.
	loopMaxPeriod = 1024
	// loopMinUnit ignores trivial 1-2 char cycles (newlines, "..") that show up
	// in valid output (indentation, ascii rules) and aren't real loops.
	loopMinUnit = 3
	// loopCutBailAt: consecutive turns cut for repetition before the loop bails
	// with RepeatStreamError. nudge first, stop when the model is clearly wedged.
	loopCutBailAt = 3
)

// degenerateTailPeriod returns the period of a repeated unit at the tail of s,
// or 0 if the tail isn't a degenerate loop. A non-zero return p means the final
// p*loopMinReps bytes consist of the same p-byte unit repeated.
func degenerateTailPeriod(s string) int {
	if len(s) > loopScanWindow {
		s = s[len(s)-loopScanWindow:]
	}
	// reject trivial cycles first: a single-char run (separators like "====",
	// whitespace) or a 1-2 char alternation is periodic at every larger period,
	// so it would otherwise match below. these appear in valid output — only
	// multi-char phrase loops count as degenerate.
	for q := 1; q < loopMinUnit; q++ {
		if isPeriodicSuffix(s, q, loopMinReps) {
			return 0
		}
	}
	maxP := len(s) / loopMinReps
	if maxP > loopMaxPeriod {
		maxP = loopMaxPeriod
	}
	for p := loopMinUnit; p <= maxP; p++ {
		if isPeriodicSuffix(s, p, loopMinReps) {
			return p
		}
	}
	return 0
}

// isPeriodicSuffix reports whether the final p*reps bytes of s repeat with
// period p — a cheap byte compare walking back from the end.
func isPeriodicSuffix(s string, p, reps int) bool {
	span := p * reps
	if span > len(s) {
		return false
	}
	tail := s[len(s)-span:]
	for i := p; i < len(tail); i++ {
		if tail[i] != tail[i-p] {
			return false
		}
	}
	return true
}

// trimLoopedTail collapses a detected repetition loop: keeps everything up to
// the first two cycles of the looped unit, then a marker. Stops dozens of
// identical lines from polluting the transcript and the next turn's context.
func trimLoopedTail(s string, period int) string {
	if period <= 0 || period >= len(s) {
		return s
	}
	// walk back while the period holds to find where the loop begins.
	start := len(s)
	for j := len(s) - 1; j-period >= 0 && s[j] == s[j-period]; j-- {
		start = j - period
	}
	keep := start + 2*period
	if keep >= len(s) {
		return s
	}
	return s[:keep] + "\n…[truncated: output repeated the same text in a loop]"
}
