// Package caveman injects token-compression rules into the system prompt.
//
// Caveman is prompt-injection, not code. The four levels (off/lite/full/ultra)
// produce progressively terser model output. Rules text is embedded at build
// time from rules/*.md so the binary stays self-contained.
//
// Compression rules adapted from JuliusBrussee/caveman
// (https://github.com/JuliusBrussee/caveman).
package caveman

import (
	"embed"
	"fmt"
	"strings"
)

// Level selects the compression intensity.
type Level string

const (
	Off   Level = "off"
	Lite  Level = "lite"
	Full  Level = "full"
	Ultra Level = "ultra"
)

// Default is the level used when none specified.
const Default = Full

//go:embed rules/*.md
var rulesFS embed.FS

// Rules returns the rules markdown for the given level.
// Off returns "". Unknown levels return "" as well — callers should validate
// with ParseLevel first.
func Rules(level Level) string {
	b, err := rulesFS.ReadFile("rules/" + string(level) + ".md")
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseLevel validates user input. Empty string → Default.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return Default, nil
	case "off", "none", "disabled":
		return Off, nil
	case "lite", "light":
		return Lite, nil
	case "full", "default":
		return Full, nil
	case "ultra", "max":
		return Ultra, nil
	default:
		return "", fmt.Errorf("caveman: unknown level %q (want off|lite|full|ultra)", s)
	}
}
