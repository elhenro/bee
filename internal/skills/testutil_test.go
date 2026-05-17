package skills

import "os"

// shared test helpers
func writeFileBytes(p string, b []byte) error {
	return os.WriteFile(p, b, 0o644)
}
