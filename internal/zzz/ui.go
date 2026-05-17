package zzz

// UI abstracts the live status sink so Drive can be driven by either the
// terminal-line Status renderer or a bubbletea TUI. Methods mirror the
// original *Status surface 1:1 so the existing impl is automatically a UI.
type UI interface {
	SetIter(n, max int)
	SetPhase(p string)
	SetTokens(t TokenStat)
	IncCommits()
	Println(msg string)
	RenderSummary(r *Run)
}

// Steerable is implemented by UIs that accept mid-run operator input. When
// Drive holds a UI that also satisfies this interface it drains Steer between
// iterations: notes get appended to the prompt, stop closes the graceful
// stop channel.
type Steerable interface {
	Steer() <-chan Steer
}

// Steer is one operator command pushed from the UI into Drive between
// iterations. Free-text falls under SteerNote — it gets appended verbatim
// to the next iteration's prompt as an "operator nudge" block.
type Steer struct {
	Kind string
	Text string
}

const (
	SteerNote  = "note"  // append text to next prompt
	SteerStop  = "stop"  // graceful stop after current iter
	SteerAbort = "abort" // hard cancel ctx mid-iter
)
