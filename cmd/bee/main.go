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

	"golang.org/x/term"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/llm"
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

// stripVerboseFlag pulls --verbose / -v out of os.Args (before any --) and
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
	reg := skills.NewRegistry()
	_ = reg.Load(skills.BaseDir())
	s, ok := reg.Get(name)
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
