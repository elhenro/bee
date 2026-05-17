// Package caveman injects token-compression rules into the system prompt.
//
// Caveman is prompt-injection, not code. The four levels (off/lite/full/ultra)
// produce progressively terser model output. Rules text is embedded at build
// time from rules/*.md so the binary stays self-contained.
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
	if level == Off {
		return ""
	}
	name, ok := fileFor(level)
	if !ok {
		return ""
	}
	b, err := rulesFS.ReadFile(name)
	if err != nil {
		return ""
	}
	return string(b)
}

// Inject prepends the level's rules to systemPrompt.
// Returns systemPrompt unchanged when level is Off or rules empty.
// Each call prepends — caller decides idempotency.
func Inject(systemPrompt string, level Level) string {
	rules := Rules(level)
	if rules == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return rules
	}
	// trailing newline on rules + blank line separator
	if !strings.HasSuffix(rules, "\n") {
		rules += "\n"
	}
	return rules + "\n" + systemPrompt
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

// fileFor maps level to embedded path.
func fileFor(level Level) (string, bool) {
	switch level {
	case Lite:
		return "rules/lite.md", true
	case Full:
		return "rules/full.md", true
	case Ultra:
		return "rules/ultra.md", true
	default:
		return "", false
	}
}
