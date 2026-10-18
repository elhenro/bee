//go:build windows

package auth

// LockToken is a no-op on Windows; refreshes serialize per process only.
func LockToken(dir, provider string) func() { return func() {} }
