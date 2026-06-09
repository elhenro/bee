//go:build windows

package cost

// lockLifetimeFile is a no-op on Windows; lifetime totals keep last-writer-
// wins semantics there (drift on concurrent processes, never corruption —
// the write itself is an atomic rename).
func lockLifetimeFile(string) func() {
	return func() {}
}
