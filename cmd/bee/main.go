// Command bee is a pure-Go self-learning minimalist coding agent.
//
// Subcommand dispatch is intentionally tiny — stdlib only. Unknown
// arg[1] falls through to skill lookup so `bee <skill> ...` runs the
// named skill non-interactively.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tui"
)

func init() {
	// forward the build-tag-injected version into tui so the intro
	// placeholder renders the live version after the animation ends.
	tui.Version = version
	// commit feeds the background update checker so it can compare the
	// running build's sha against main HEAD. Dev/source builds (commit
	// "" or "dev") skip the probe entirely.
	tui.Commit = commit
}

// version and commit are overridable at link time. The release workflow
// injects the tag name and short SHA:
//
//	go build -ldflags="-X main.version=$TAG -X main.commit=$SHA"
var (
	version = "0.1.1"
	commit  = "dev"
)

func main() {
	stripVerboseFlag()
	stripCavemanFlag()
	stripEffortFlag()
	if len(os.Args) < 2 {
		runTUI()
		return
	}
	switch os.Args[1] {
	case "run", "-p", "--print":
		runHeadless(os.Args[2:])
	case "back":
		back(os.Args[2:])
	case "fan":
		runFan(os.Args[2:])
	case "swarm":
		runSwarm(os.Args[2:])
	case "hyperplan":
		runHyperplan(os.Args[2:])
	case "hive":
		runHive()
	case "explain":
		runExplain(os.Args[2:])
	case "quiz":
		runQuiz(os.Args[2:])
	case "bg":
		runBg(os.Args[2:])
	case "agents":
		runAgents()
	case "remote-control":
		runRemoteControl(os.Args[2:])
	case "zzz":
		runZzz(os.Args[2:])
	case "cron":
		runCron(os.Args[2:])
	case "doctor":
		runDoctor(os.Args[2:])
	case "bench":
		runBench(os.Args[2:])
	case "waggle":
		runWaggle(os.Args[2:])
	case "version", "-v", "--version":
		if commit != "" && commit != "dev" {
			fmt.Printf("bee %s (%s)\n", version, commit)
		} else {
			fmt.Println("bee", version)
		}
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

// failf prints a line to stderr and returns code, for paths that unwind their
// defers before the process exits.
func failf(code int, format string, a ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	return code
}

// exitf is failf followed by an immediate exit.
func exitf(code int, format string, a ...any) { os.Exit(failf(code, format, a...)) }

// parseFlags parses args onto fs, reporting errors on stderr. A bad flag exits 2.
func parseFlags(fs *flag.FlagSet, args []string) {
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`bee — pure-Go self-learning minimalist coding agent

usage:
  bee                       start interactive TUI session
  bee back [session-id]     resume a TUI session by id (defaults to latest)
  bee run [flags] <msg>     run one task headless, stream stdout, exit
  bee -p   [flags] <msg>    alias for 'bee run' (print mode, for scripts/pipes)
  bee fan  [flags] <task>   fan out N parallel bees over a workload
  bee swarm <task>          queen + workers for a complex task
  bee hyperplan <plan>      spawn 5 critics to attack a plan
  bee hive                  list active bees and recent sessions
  bee explain [session-id]  review guide: what each request changed and why
  bee quiz [session-id]     guess each change's reason, then reveal it
  bee bg [--skill <name>] <message>  run a task in the background
  bee bg --list                      list background bees
  bee bg --tail <id>                 follow a background log
  bee bg --kill <id>                 stop a background bee
  bee agents                        parallel-agents overview (worktree-per-agent)
  bee remote-control [--port N] [--yes]  serve a local web relay (URL + QR) to drive bee from another device
  bee zzz [flags] <objective>        overnight loop: clean→prompt→commit-or-reset
  bee zzz --agents N --yes <obj>     queen mode: N parallel worker bees, one dashboard
  bee zzz --list                     list overnight runs
  bee zzz --resume <id>              resume an aborted run
  bee zzz --gc [--gc-max-age <d>] [--gc-keep <n>]  prune terminal runs + bg sessions
  bee cron ls|run <name>|daemon      scheduled jobs from [[cron.jobs]] in config
  bee doctor [--json]       preflight: dirs, sandbox, provider creds
  bee explore <target>      trace a file/symbol/concept and print a markdown map
  bee research <topic>      deep multi-source web research, structured report
  bee <skill> [args...]     run a skill non-interactively
  bee version               print version
  bee help                  show this help

global flags (any position):
  --verbose                 show full tool output
  --caveman <lvl>           force caveman level: off|lite|full|ultra
  --effort <lvl>            force thinking level: auto|off|low|medium|high|max
`)
}

func back(args []string) {
	// no id = resume most recent session
	id := "latest"
	if len(args) >= 1 && args[0] != "" {
		id = args[0]
	}
	if id == "latest" || id == "l" {
		sessions, err := session.List()
		if err != nil {
			exitf(1, "bee back: list sessions: %v", err)
		}
		if len(sessions) == 0 {
			exitf(1, "bee back: no sessions found")
		}
		id = sessions[0].ID
	}
	if p, err := session.Path(id); err != nil {
		exitf(1, "bee back: %v", err)
	} else if _, err := os.Stat(p); err != nil {
		exitf(1, "bee back: session %s not found", id)
	}
	runTUIWithSession(id, "")
}

// dispatchSkill resolves name against ~/.bee/skills. Returns false if
// the skill doesn't exist so main can fall through to "unknown command".
//
// Surfacing: a prompt skill run from an interactive terminal opens the TUI
// seeded with "/<name> <args>" so the user watches thoughts + tool cards live
// and can steer. Piped/redirected stdio (or non-prompt kinds) stays headless
// so `bee research foo > out.md` and scripts keep working.
func dispatchSkill(name string, rest []string) bool {
	ensureFirstRun()
	s, ok := newSkillRegistry().Get(name)
	if !ok {
		// crystallized routes: a recipe waggle (shell steps that change
		// state) goes through the headless engine so each step meets the
		// approval gate.
		if s, ok = waggleSkill(name); !ok {
			return false
		}
	}
	// an exec skill (installed or waggle) runs directly, no model turn: sending
	// it to the engine as chat would never execute the command.
	if s.Kind == skills.KindExec {
		runExecWaggle(s, rest)
		return true
	}
	// prompt and legacy recipe skills run in the TUI when stdio is
	// interactive so the user sees thoughts + tool cards live and can steer.
	// exec skills are auto-registered as tools and don't have a "bee <name>"
	// form worth seeding the TUI with; a runtime recipe executes in Go and
	// stays headless. Piped/redirected stdio stays headless so
	// `bee research foo > out.md` and scripts keep working.
	modelWalked := s.Kind == skills.KindPrompt || (s.Kind == skills.KindRecipe && !s.Runtime)
	if modelWalked && stdioIsInteractive() {
		seed := "/" + name
		if joined := strings.TrimSpace(strings.Join(rest, " ")); joined != "" {
			seed += " " + joined
		}
		runTUIWithSession("", seed)
		return true
	}
	// translate to the headless engine: `bee run --skill <name> -- <rest...>`
	args := append([]string{"--skill", name, "--"}, rest...)
	runHeadless(args)
	return true
}

// stdioIsInteractive reports whether both stdin and stdout are terminals.
// Requires both: the seeded TUI auto-submits to stdout but still needs a tty
// stdin for steering and approvals.
func stdioIsInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}
