// Package atomicfile replaces a file's contents without ever exposing a
// partial write.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write writes data to a unique temp file beside path, then renames it into
// place: a concurrent reader never sees a half-written file, a crash mid-write
// can't leave a torn one, and the unique name keeps goroutines or processes
// writing the same target from clobbering each other's temp.
func Write(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, perm)
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		_ = os.Remove(tmp)
	}
	return werr
}
