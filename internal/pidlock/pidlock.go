// Package pidlock is a single-host advisory lock on a file. The file records
// the holder as "<pid>\n<info>" for diagnostics, but liveness never depends on
// it: on unix the kernel flock decides, and the file is never unlinked, so a
// crashed holder frees the lock and a racing waiter can never end up with two
// holders (unlink-then-recreate would let one waiter lock the old inode while
// another locks the new one).
package pidlock

import "fmt"

// HeldError reports that a live process holds the lock.
type HeldError struct {
	Path string
	Info string // holder record, "<pid>\n<info>"; may be empty during a race
}

func (e *HeldError) Error() string { return "lock held: " + e.Path }

// Lock is a held lock. Release is safe on a nil receiver.
type Lock struct {
	impl lockImpl
}

// Release frees the lock.
func (l *Lock) Release() {
	if l == nil {
		return
	}
	l.impl.release()
}

func record(pid int, info string) string { return fmt.Sprintf("%d\n%s", pid, info) }
