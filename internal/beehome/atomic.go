package beehome

import (
	"os"
	"path/filepath"
)

// WriteAtomic replaces path with data via a temp file in the same directory
// plus rename, so a crash or a concurrent reader never sees a torn file.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if err = os.Chmod(f.Name(), perm); err == nil {
			err = os.Rename(f.Name(), path)
		}
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}
