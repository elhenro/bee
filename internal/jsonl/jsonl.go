// Package jsonl appends records to JSON-lines logs.
package jsonl

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Append writes v as one line at the end of the file at path, creating the file
// and its parent dirs on demand. A single O_APPEND write of a short line is
// atomic on local filesystems, so a crash mid-write can at worst leave one
// trailing partial line, which readers skip.
func Append(path string, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
