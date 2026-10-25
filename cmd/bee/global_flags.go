// Global flags (--verbose, --caveman, --effort) are stripped from os.Args at
// any position before subcommand dispatch and carried in the environment.
package main

import (
	"os"
	"strings"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/llm"
)

// stripVerboseFlag pulls --verbose out of os.Args (before any --) and
// flips BEE_VERBOSE=1 so both the TUI and headless paths render full tool
// output. Token isn't passed through to subcommands since flag.Parse would
// fail on the rare overlap.
func stripVerboseFlag() {
	out := os.Args[:1]
	hit := false
	for i, a := range os.Args[1:] {
		if a == "--" { // everything after is the message, not flags
			out = append(out, os.Args[1+i:]...)
			break
		}
		if a == "--verbose" {
			hit = true
			continue
		}
		out = append(out, a)
	}
	if hit {
		_ = os.Setenv("BEE_VERBOSE", "1")
		os.Args = out
	}
}

// stripValueFlag pulls --name <val> / --name=<val> (also single-dash) out of
// os.Args at any position before a bare --, so it works as a global flag. The
// last hit wins; os.Args is rewritten only on a hit. A missing value exits 2.
func stripValueFlag(name, levels string) (val string, hit bool) {
	out := os.Args[:1]
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		switch {
		case a == "--"+name || a == "-"+name:
			if i+1 >= len(args) {
				exitf(2, "bee: --%s needs a level (%s)", name, levels)
			}
			i++
			val, hit = args[i], true
		case strings.HasPrefix(a, "--"+name+"=") || strings.HasPrefix(a, "-"+name+"="):
			val, hit = a[strings.IndexByte(a, '=')+1:], true
		default:
			out = append(out, a)
		}
	}
	if hit {
		os.Args = out
	}
	return val, hit
}

// stripCavemanFlag handles the global --caveman <lvl> flag (e.g. `bee --caveman
// full run msg`). The value is validated up front and stashed in BEE_CAVEMAN;
// the TUI and headless paths pick it up after config.Load. Invalid level exits 2.
func stripCavemanFlag() {
	val, hit := stripValueFlag("caveman", "off|lite|full|ultra")
	if !hit {
		return
	}
	lvl, err := caveman.ParseLevel(val)
	if err != nil {
		exitf(2, "bee: %v", err)
	}
	_ = os.Setenv("BEE_CAVEMAN", string(lvl))
}

// stripEffortFlag handles the global --effort <lvl> flag (e.g. `bee --effort
// high run msg`). Value validated up front and stashed in BEE_EFFORT; both TUI
// and headless paths consume it after config.Load. Invalid level exits 2.
// Subcommand-local --thinking still works via the run flagset and takes
// precedence over BEE_EFFORT.
func stripEffortFlag() {
	val, hit := stripValueFlag("effort", "auto|off|low|medium|high|max")
	if !hit {
		return
	}
	canonical := llm.ParseThinking(val)
	// ParseThinking returns Off for unknown — reject silent typos.
	if canonical == llm.ThinkingOff && strings.ToLower(strings.TrimSpace(val)) != "off" {
		exitf(2, "bee: --effort: unknown level %q (want auto|off|low|medium|high|max)", val)
	}
	_ = os.Setenv("BEE_EFFORT", string(canonical))
}
