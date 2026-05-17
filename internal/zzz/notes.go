package zzz

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReadNotes returns the current notes.md contents (empty on first iter).
func ReadNotes(id string) (string, error) {
	dir, err := RunDir(id)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

// AppendNote tacks one section onto notes.md. Used after every iteration so
// later prompts can see what prior iterations did.
func AppendNote(id string, iter int, subject, body string) error {
	dir, err := RunDir(id)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "notes.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var b strings.Builder
	fmt.Fprintf(&b, "\n## iter %d — %s\n", iter, strings.TrimSpace(subject))
	fmt.Fprintf(&b, "_%s_\n\n", time.Now().UTC().Format(time.RFC3339))
	if body = strings.TrimSpace(body); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	_, err = f.WriteString(b.String())
	return err
}
