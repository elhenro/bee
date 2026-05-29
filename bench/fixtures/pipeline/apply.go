package pipeline

// Apply runs the pipeline over n. It currently only increments; the Double step
// is not wired in yet.
func Apply(n int) int {
	return Inc(n)
}
