package hive

// Hooks observes a Queen run as it progresses. Every callback is optional;
// nil callbacks are skipped, so existing callers (bee swarm) pass a zero
// Hooks and see unchanged behavior. Callbacks run on the goroutine driving
// the phase (OnWorkerStart/Done fire from worker goroutines), so keep them
// cheap and non-blocking (a channel send the UI drains is ideal).
type Hooks struct {
	OnPlan        func(plan []SubTask)
	OnWorkerStart func(idx int, sub SubTask)
	OnWorkerDone  func(idx int, res Result)
	OnReview      func(dimension string, claims []string)
	OnVerify      func(f Finding)
	OnSynthesize  func()
}

// fire calls an optional hook; a nil hook is skipped.
func fire[A any](f func(A), a A) {
	if f != nil {
		f(a)
	}
}

func fire2[A, B any](f func(A, B), a A, b B) {
	if f != nil {
		f(a, b)
	}
}
