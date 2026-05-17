// Command bee is a pure-Go self-learning minimalist coding agent.
//
// Subcommand dispatch is intentionally tiny — stdlib only. Unknown
// arg[1] falls through to skill lookup so `bee <skill> ...` runs the
// named skill non-interactively.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
)

const version = "0.1.0"

// reserved names cannot be used as skill names — they always dispatch
// to the built-in subcommand. Lookup must check this before consulting
// the skill registry.
var reservedSubcommands = map[string]bool{
	"run":       true,
	"back":      true,
	"fan":       true,
	"swarm":     true,
	"hyperplan": true,
	"hive":      true,
	"bg":        true,
	"doctor":    true,
	"version":   true,
	"-v":        true,
	"--version": true,
	"help":      true,
	"-h":        true,
	"--help":    true,
	"-p":        true,
	"--print":   true,
}

func main() {
	stripVerboseFlag()
	stripCavemanFlag()
	if len(os.Args) < 2 {
		repl()
		return
	}
	switch os.Args[1] {
	case "run", "-p", "--print":
		runHeadless(os.Args[2:])
	case "back":
		back(os.Args[2:])
	case "fan":
		fan(os.Args[2:])
	case "swarm":
		swarm(os.Args[2:])
	case "hyperplan":
		runHyperplan(os.Args[2:])
	case "hive":
		hive(os.Args[2:])
	case "bg":
		bg(os.Args[2:])
	case "doctor":
		runDoctor(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("bee", version)
	case "help", "-h", "--help":
		usage()
	default:
		// fall through: try `bee <skill> ...`
		if !dispatchSkill(os.Args[1], os.Args[2:]) {
			fmt.Fprintf(os.Stderr, "bee: unknown command %q\n", os.Args[1])
			usage()
			os.Exit(2)
		}
	}
}

func usage() {
	fmt.Print(`bee — pure-Go self-learning minimalist coding agent

usage:
  bee                       start interactive TUI session
  bee back <session-id>     resume a prior TUI session by id (or 'latest')
  bee run [flags] <msg>     run one task headless, stream stdout, exit
  bee -p   [flags] <msg>    alias for 'bee run' (print mode, for scripts/pipes)
  bee fan  [flags] <task>   fan out N parallel bees over a workload
  bee swarm <task>          queen + workers for a complex task
  bee hyperplan <plan>      spawn 5 critics to attack a plan
  bee hive                  list active bees and recent sessions
  bee bg [--skill <name>] <message>  run a task in the background
  bee bg --list                      list background bees
  bee bg --tail <id>                 follow a background log
  bee bg --kill <id>                 stop a background bee
  bee doctor [--json]       preflight: dirs, sandbox, provider creds
  bee <skill> [args...]     run a skill non-interactively
  bee version               print version
  bee help                  show this help

global flags (any position):
  --verbose                 show full tool output
  --caveman <lvl>           force caveman level: off|lite|full|ultra
`)
}

func repl() {
	runTUI()
}

// stripVerboseFlag pulls --verbose / -v out of os.Args (any position) and
// flips BEE_VERBOSE=1 so both the TUI and headless paths render full tool
// output. Token isn't passed through to subcommands since flag.Parse would
// fail on the rare overlap.
func stripVerboseFlag() {
	out := os.Args[:1]
	hit := false
	for _, a := range os.Args[1:] {
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

// stripCavemanFlag pulls --caveman <lvl> / --caveman=<lvl> (also -caveman
// variants) out of os.Args at any position so it works as a global flag
// (e.g. `bee --caveman full`, `bee --caveman full run msg`). The value is
// validated up front and stashed in BEE_CAVEMAN; the TUI and headless paths
// pick it up after config.Load. Invalid level exits 2.
func stripCavemanFlag() {
	out := os.Args[:1]
	var val string
	hit := false
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--caveman" || a == "-caveman":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "bee: --caveman needs a level (off|lite|full|ultra)")
				os.Exit(2)
			}
			val = args[i+1]
			i++
			hit = true
		case strings.HasPrefix(a, "--caveman=") || strings.HasPrefix(a, "-caveman="):
			val = a[strings.IndexByte(a, '=')+1:]
			hit = true
		default:
			out = append(out, a)
		}
	}
	if !hit {
		return
	}
	lvl, err := caveman.ParseLevel(val)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee: %v\n", err)
		os.Exit(2)
	}
	_ = os.Setenv("BEE_CAVEMAN", string(lvl))
	os.Args = out
}

func runHeadless(args []string) {
	runHeadlessReal(args)
}

func back(args []string) {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "bee back: missing <session-id>")
		fmt.Fprintln(os.Stderr, "usage: bee back <session-id>")
		os.Exit(2)
	}
	id := args[0]
	if id == "latest" || id == "l" {
		sessions, err := session.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee back: list sessions: %v\n", err)
			os.Exit(1)
		}
		if len(sessions) == 0 {
			fmt.Fprintln(os.Stderr, "bee back: no sessions found")
			os.Exit(1)
		}
		id = sessions[0].ID
	}
	if p, err := session.Path(id); err != nil {
		fmt.Fprintf(os.Stderr, "bee back: %v\n", err)
		os.Exit(1)
	} else if _, err := os.Stat(p); err != nil {
		fmt.Fprintf(os.Stderr, "bee back: session %s not found\n", id)
		os.Exit(1)
	}
	runTUIWithSession(id)
}

func fan(args []string)   { runFan(args) }
func swarm(args []string) { runSwarm(args) }
func hive(args []string) { runHive(args) }
func bg(args []string)    { runBg(args) }

// dispatchSkill resolves name against ~/.bee/skills. Returns false if
// the skill doesn't exist so main can fall through to "unknown command".
// Reserved names short-circuit before we even read the registry.
func dispatchSkill(name string, rest []string) bool {
	if reservedSubcommands[name] {
		return false
	}
	ensureFirstRun()
	reg := skills.NewRegistry()
	_ = reg.Load(skills.BaseDir())
	if _, ok := reg.Get(name); !ok {
		return false
	}
	// translate to the headless engine: `bee run --skill <name> -- <rest...>`
	args := append([]string{"--skill", name, "--"}, rest...)
	runHeadless(args)
	return true
}
