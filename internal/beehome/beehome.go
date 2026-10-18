// Package beehome resolves bee's state root. Leaf package (stdlib only) so
// config, auth, session and knowledge can all share it without import cycles.
package beehome

import (
	"os"
	"path/filepath"
)

// Dir returns $BEE_HOME when set, else ~/.bee.
func Dir() (string, error) {
	if v := os.Getenv("BEE_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bee"), nil
}

// Join is Dir joined with elem.
func Join(elem ...string) (string, error) {
	root, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{root}, elem...)...), nil
}
