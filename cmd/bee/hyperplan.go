// `bee hyperplan` — adversarial planning swarm.
//
// 5 Critic engines attack a plan from distinct angles (security, perf,
// complexity, scope, edge cases), then a Synthesizer (Queen) consolidates
// the critiques into a refined plan.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/config"
	hivepkg "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
)

// hyperplanOpts captures parsed flags. Number of critics defaults to 5 to
// match the 5 attack angles below; smaller N reuses the angles round-robin.
type hyperplanOpts struct {
	N        int
	Model    string
	Provider string
}

// attackAngles is the canonical list of 5 critic angles. Order is stable —
// tests + cli help depend on it.
var attackAngles = []struct {
	Name string
	Desc string
}{
	{"security", "attack from a security/threat-model perspective"},
	{"performance", "attack from a perf, scaling, resource-cost perspective"},
	{"complexity", "attack from an over-engineering / YAGNI / maintainability perspective"},
	{"scope", "attack from scope-creep / wrong-problem perspective"},
	{"edge-cases", "enumerate failure modes and unhandled scenarios"},
}

// parseHyperplanArgs splits flags from the positional plan. Mirrors bg.go's
// parseBgArgs so cmd-level tests can exercise flag parsing without spawning
// engines.
func parseHyperplanArgs(args []string) (string, hyperplanOpts, error) {
	fs := flag.NewFlagSet("hyperplan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	n := fs.Int("n", 5, "number of critic bees (default 5; angles cycle if N!=5)")
	model := fs.String("model", "", "override config default_model for all critics + synthesizer")
	providerName := fs.String("provider", "", "override config default_provider")
	if err := fs.Parse(args); err != nil {
		return "", hyperplanOpts{}, err
	}
	msg := strings.TrimSpace(strings.Join(fs.Args(), " "))
	opts := hyperplanOpts{N: *n, Model: *model, Provider: *providerName}
	if msg == "" {
		return "", opts, errors.New("missing <plan>: usage: bee hyperplan [--n 5] [--model M] <plan>")
	}
	if opts.N < 1 {
		return "", opts, errors.New("--n must be ≥1")
	}
	return msg, opts, nil
}

// runHyperplan is the entry point wired into main.go.
func runHyperplan(args []string) {
	plan, opts, err := parseHyperplanArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee hyperplan: %v\n", err)
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		if os.Getenv("BEE_TEST_PROVIDER") == "stub" {
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "bee hyperplan: config: %v\n", err)
			os.Exit(1)
		}
	}
	applyOverrides(&cfg, opts.Model, opts.Provider, "")

	prov, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee hyperplan: provider: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	rawReg, err := buildTools(cwd, cfg, prov, storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee hyperplan: tools: %v\n", err)
		os.Exit(1)
	}

	// critics are read-only — they reason about a plan, they don't touch disk.
	criticAllowed := hivepkg.RoleCritic.AllowedTools()
	criticReg, err := filterTools(rawReg, strings.Join(criticAllowed, ","))
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee hyperplan: filter critic tools: %v\n", err)
		os.Exit(1)
	}

	skillReg := skills.NewRegistry()
	_ = skillReg.Load(skills.BaseDir())

	// build N critic engines + 1 synthesizer engine.
	critics := make([]hivepkg.Runner, 0, opts.N)
	prompts := make([]string, 0, opts.N)
	closers := make([]io.Closer, 0, opts.N+1)
	for i := 0; i < opts.N; i++ {
		eng, sess, err := newCriticEngine(prov, criticReg, skillReg, cfg, cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee hyperplan: critic %d session: %v\n", i, err)
			os.Exit(1)
		}
		critics = append(critics, eng)
		closers = append(closers, sess)
		prompts = append(prompts, criticPrompt(plan, i))
	}

	synth, synthSess, err := newSynthEngine(prov, rawReg, skillReg, cfg, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee hyperplan: synth session: %v\n", err)
		os.Exit(1)
	}
	closers = append(closers, synthSess)
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	fmt.Fprintf(os.Stderr, "hyperplan: dispatching %d critics...\n", opts.N)
	critiques := runCritics(ctx, critics, prompts)

	fmt.Fprintln(os.Stderr, "hyperplan: synthesizing refined plan...")
	refined, err := synthesize(ctx, synth, plan, critiques)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee hyperplan: synthesize: %v\n", err)
		printCritiques(os.Stderr, critiques)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, refined)
	printCritiques(os.Stderr, critiques)
}

// criticResult pairs a critic's angle with its output (or error).
type criticResult struct {
	Angle string
	Text  string
	Err   error
}

// criticPrompt wedges the angle + plan into the user message. The critic
// role's SystemPrompt() is also prefixed so the engine inherits the
// adversarial persona without needing a system-prompt injection seam.
func criticPrompt(plan string, idx int) string {
	angle := attackAngles[idx%len(attackAngles)]
	var b strings.Builder
	b.WriteString(hivepkg.RoleCritic.SystemPrompt())
	b.WriteString("\n\nplan to review:\n<<<\n")
	b.WriteString(plan)
	b.WriteString("\n>>>\n\nyour assignment: ")
	b.WriteString(angle.Desc)
	b.WriteString(". list every concrete flaw you find. do NOT propose fixes. cite specific risks.")
	return b.String()
}

// runCritics fans the N critic engines out in parallel goroutines, bounded
// by N itself — there's no benefit to limiting concurrency further at this
// scale, and matching the Queen's direct-goroutine pattern keeps the code
// surface small.
func runCritics(ctx context.Context, critics []hivepkg.Runner, prompts []string) []criticResult {
	out := make([]criticResult, len(critics))
	var wg sync.WaitGroup
	for i := range critics {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			angle := attackAngles[idx%len(attackAngles)].Name
			res := criticResult{Angle: angle}
			run, err := critics[idx].Run(ctx, prompts[idx])
			if err != nil {
				res.Err = err
			} else {
				res.Text = strings.TrimSpace(run.FinalText)
			}
			out[idx] = res
		}(i)
	}
	wg.Wait()
	return out
}

// synthesize hands all critiques to the Queen for a refined plan.
func synthesize(ctx context.Context, synth hivepkg.Runner, plan string, critiques []criticResult) (string, error) {
	var b strings.Builder
	b.WriteString(hivepkg.RoleQueen.SystemPrompt())
	b.WriteString("\n\noriginal plan:\n<<<\n")
	b.WriteString(plan)
	b.WriteString("\n>>>\n\n")
	b.WriteString(fmt.Sprintf("you have received %d adversarial critiques attacking this plan. consolidate them into a refined plan that addresses the substantive flaws. ignore noise. output as: 'Refined Plan' header, then numbered steps, then a 'Risks accepted' section for flaws you chose not to mitigate.\n\n", len(critiques)))
	for i, c := range critiques {
		fmt.Fprintf(&b, "## Critique %d — %s angle\n", i+1, c.Angle)
		if c.Err != nil {
			fmt.Fprintf(&b, "(error: %v)\n\n", c.Err)
			continue
		}
		b.WriteString(c.Text)
		b.WriteString("\n\n")
	}
	run, err := synth.Run(ctx, b.String())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(run.FinalText), nil
}

// printCritiques dumps raw critic outputs as a "see also" section. Goes to
// stderr so refined plan on stdout stays pipe-friendly.
func printCritiques(w io.Writer, critiques []criticResult) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "see also — raw critiques:")
	for i, c := range critiques {
		fmt.Fprintf(w, "\n--- critique %d (%s) ---\n", i+1, c.Angle)
		if c.Err != nil {
			fmt.Fprintf(w, "error: %v\n", c.Err)
			continue
		}
		fmt.Fprintln(w, c.Text)
	}
}

// newCriticEngine builds a read-only Engine with its own session rollout.
// memory is intentionally skipped — critics reason about the supplied plan,
// not the user's notes.
func newCriticEngine(prov llm.Provider, reg *tools.Registry, skillReg *skills.Registry, cfg config.Config, cwd string) (*loop.Engine, *session.Rollout, error) {
	sessID := uuid.NewString()
	roll, err := session.Open(sessID)
	if err != nil {
		return nil, nil, err
	}
	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Cfg:      cfg,
		Cwd:      cwd,
		Sessions: roll,
		Stdout:   io.Discard,
	}
	return eng, roll, nil
}

// newSynthEngine builds the Queen synthesizer. Full tool registry so it can
// optionally verify claims, but it usually doesn't need to.
func newSynthEngine(prov llm.Provider, reg *tools.Registry, skillReg *skills.Registry, cfg config.Config, cwd string) (*loop.Engine, *session.Rollout, error) {
	sessID := uuid.NewString()
	roll, err := session.Open(sessID)
	if err != nil {
		return nil, nil, err
	}
	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Cfg:      cfg,
		Cwd:      cwd,
		Sessions: roll,
		Stdout:   io.Discard,
	}
	return eng, roll, nil
}
