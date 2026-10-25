package apply_patch

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// fileState is one path as the batch sees it: the on-disk content until a
// section touches it, the planned content after.
type fileState struct {
	data   []byte
	mode   os.FileMode
	exists bool
	orig   bool // existed on disk before the batch
}

// overlay holds every file a batch touches in memory. Nothing hits disk until
// commit, so a mid-batch failure leaves the tree untouched (all-or-nothing)
// and later sections see the result of earlier ones.
type overlay struct {
	files map[string]*fileState
	order []string
}

func newOverlay() *overlay { return &overlay{files: map[string]*fileState{}} }

func (o *overlay) get(path string) (*fileState, error) {
	if st, ok := o.files[path]; ok {
		return st, nil
	}
	st := &fileState{mode: 0o644}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	case info.IsDir():
		return nil, fmt.Errorf("%s is a directory", path)
	default:
		if st.data, err = os.ReadFile(path); err != nil {
			return nil, err
		}
		st.exists, st.orig, st.mode = true, true, info.Mode().Perm()
	}
	o.files[path] = st
	o.order = append(o.order, path)
	return st, nil
}

// existing is get for a path the patch needs to find on disk (or earlier in
// the batch).
func (o *overlay) existing(path string) (*fileState, error) {
	st, err := o.get(path)
	if err != nil {
		return nil, err
	}
	if !st.exists {
		return nil, fmt.Errorf("open %s: no such file or directory", path)
	}
	return st, nil
}

// applyDiff applies one parsed File to src and returns the new content.
func applyDiff(f *gitdiff.File, src []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := gitdiff.Apply(&buf, bytes.NewReader(src), f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// plan applies one parsed File to the overlay and returns its summary row.
// It validates fully before mutating, so a failed section changes nothing.
func (o *overlay) plan(f *gitdiff.File) (fileChange, error) {
	if f.IsBinary {
		return fileChange{}, errors.New("binary patches not supported")
	}
	added, removed := lineCounts(f)
	switch {
	case f.IsDelete:
		return o.planDelete(f, removed)
	case f.IsNew:
		return o.planCreate(f, added)
	case f.IsRename || f.IsCopy:
		return o.planMove(f, added, removed)
	}
	path := f.NewName
	if path == "" {
		path = f.OldName
	}
	if path == "" {
		return fileChange{}, errors.New("modify patch missing file name")
	}
	st, err := o.existing(path)
	if err != nil {
		return fileChange{}, err
	}
	data, err := applyDiff(f, st.data)
	if err != nil {
		return fileChange{}, err
	}
	// reject empty result on modify; require explicit deletion semantics
	if len(data) == 0 {
		return fileChange{}, fmt.Errorf("modify patch for %s produced empty file; use deleted file mode to remove", path)
	}
	st.data = data
	return fileChange{path: path, kind: "modify", added: added, removed: removed}, nil
}

func (o *overlay) planDelete(f *gitdiff.File, removed int) (fileChange, error) {
	if f.OldName == "" {
		return fileChange{}, errors.New("delete patch missing old name")
	}
	st, err := o.existing(f.OldName)
	if err != nil {
		return fileChange{}, err
	}
	if err := verifyDelete(f, st.data); err != nil {
		return fileChange{}, err
	}
	st.exists, st.data = false, nil
	return fileChange{path: f.OldName, kind: "delete", removed: removed}, nil
}

func (o *overlay) planCreate(f *gitdiff.File, added int) (fileChange, error) {
	if f.NewName == "" {
		return fileChange{}, errors.New("create patch missing new name")
	}
	st, err := o.get(f.NewName)
	if err != nil {
		return fileChange{}, err
	}
	if st.exists {
		return fileChange{}, fmt.Errorf("create patch but %s already exists", f.NewName)
	}
	data, err := applyDiff(f, nil)
	if err != nil {
		return fileChange{}, err
	}
	// ignore the patch-supplied mode: a model-controlled file mode could set
	// over-permissive bits. New files get a fixed safe mode.
	st.exists, st.data, st.mode = true, data, 0o644
	return fileChange{path: f.NewName, kind: "create", added: added}, nil
}

// verifyDelete checks the removed lines against the file so a delete built
// from a wrong picture of the contents fails like any other context mismatch.
// A header-only delete (no hunks) has nothing to check.
func verifyDelete(f *gitdiff.File, data []byte) error {
	if len(f.TextFragments) == 0 {
		return nil
	}
	left, err := applyDiff(f, data)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return fmt.Errorf("delete patch for %s leaves content behind; the file differs from what the patch removes", f.OldName)
	}
	return nil
}

// planMove handles rename and copy: content comes from the old path, lands on
// the new one, and a rename drops the old path. The destination must be free.
func (o *overlay) planMove(f *gitdiff.File, added, removed int) (fileChange, error) {
	if f.OldName == "" || f.NewName == "" {
		return fileChange{}, errors.New("rename/copy patch missing a file name")
	}
	src, err := o.existing(f.OldName)
	if err != nil {
		return fileChange{}, err
	}
	dst, err := o.get(f.NewName)
	if err != nil {
		return fileChange{}, err
	}
	if dst.exists {
		return fileChange{}, fmt.Errorf("rename target %s already exists", f.NewName)
	}
	data, err := applyDiff(f, src.data)
	if err != nil {
		return fileChange{}, err
	}
	dst.exists, dst.data, dst.mode = true, data, src.mode
	if f.IsRename {
		src.exists, src.data = false, nil
	}
	return fileChange{path: f.NewName, kind: "modify", added: added, removed: removed}, nil
}

// commit writes the overlay to disk. On failure it reports the path and how
// many files were already written.
func (o *overlay) commit() (path string, done, total int, err error) {
	total = len(o.order)
	for _, p := range o.order {
		st := o.files[p]
		switch {
		case !st.exists && st.orig:
			err = os.Remove(p)
		case st.exists:
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
				err = os.WriteFile(p, st.data, st.mode)
			}
		}
		if err != nil {
			return p, done, total, err
		}
		done++
	}
	return "", done, total, nil
}
