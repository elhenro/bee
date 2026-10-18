//go:build windows

package waggle

// lockFile is a no-op on Windows; the in-process mutexes still serialize
// goroutines.
func lockFile(string) func() { return func() {} }
