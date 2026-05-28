// remote-control: a local web relay. bee binds an HTTP server on the LAN;
// open the printed URL (or scan the QR) on another device to drive this
// session. Everything executes locally on this machine — no cloud proxy.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/remote"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/types"
)

// runRemoteControl serves a local web relay so another device on the LAN can
// drive this bee session. Reuses the headless engine construction.
func runRemoteControl(args []string) {
	fs := flag.NewFlagSet("remote-control", flag.ContinueOnError)
	port := fs.Int("port", 0, "listen port (0 = OS-assigned)")
	yes := fs.Bool("yes", false, "acknowledge LAN clients can run tools locally on this machine")
	model := fs.String("model", "", "override config default_model")
	providerName := fs.String("provider", "", "override config default_provider")
	sandboxScope := fs.String("sandbox", "", "override sandbox scope (read-only|workspace-write|danger-full-access)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		if os.Getenv("BEE_TEST_PROVIDER") != "" {
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "bee remote-control: config: %v\n", err)
			os.Exit(1)
		}
	}
	applyOverrides(&cfg, *model, *providerName, *sandboxScope)

	// trust gate: anyone on the LAN who reaches the URL can drive the agent
	// and run tools locally. Refuse full-access without explicit --yes.
	scope := cfg.Sandbox.Scope
	if scope == "danger-full-access" && !*yes {
		fmt.Fprintln(os.Stderr, "bee remote-control: refusing to start with sandbox scope danger-full-access.")
		fmt.Fprintln(os.Stderr, "anyone on your LAN who opens the URL can run tools on this machine with full access.")
		fmt.Fprintln(os.Stderr, "to proceed: re-run with --yes, or pick a tighter scope with --sandbox workspace-write.")
		os.Exit(2)
	}
	fmt.Fprintf(os.Stdout, "remote-control: clients execute locally under sandbox scope %s\n", scope)

	prov, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee remote-control: provider: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	app := buildHeadlessApprover(cfg, *yes)
	reg, err := buildToolsWithApprover(cwd, cfg, prov, storeDir, app)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee remote-control: tools: %v\n", err)
		os.Exit(1)
	}

	ensureFirstRun()
	skillReg := skills.NewRegistry()
	_ = skillReg.Load(skills.BaseDir())

	sessID := uuid.NewString()
	roll, err := session.Open(sessID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee remote-control: session: %v\n", err)
		os.Exit(1)
	}
	defer roll.Close()

	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Memory:   newKnowledgeAdapter(prov, cfg),
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   os.Stdout,
		Costs:    cost.New(),
	}

	adapter := &remoteEngine{eng: eng}
	srv := remote.New(adapter, remote.Options{Addr: fmt.Sprintf(":%d", *port), Title: "bee", Log: os.Stdout})
	ln, url, err := srv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee remote-control: start: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "\n  open from another device:\n  %s\n\n", url)
	if qr, err := remote.RenderQR(url); err == nil {
		fmt.Fprint(os.Stdout, qr)
		fmt.Fprintln(os.Stdout)
	}
	fmt.Fprintln(os.Stdout, "  press Ctrl+C to stop")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.Serve(ctx, ln); err != nil {
		fmt.Fprintf(os.Stderr, "bee remote-control: serve: %v\n", err)
	}
	fmt.Fprintln(os.Stdout, "remote-control: stopped")
}

// remoteEngine adapts *loop.Engine to remote.Engine. It keeps the running
// transcript across calls so the conversation accumulates; one turn at a time
// is guaranteed by the relay's busy flag.
type remoteEngine struct {
	eng  *loop.Engine
	msgs []types.Message
}

// Send runs one turn, streaming text deltas through onDelta.
func (r *remoteEngine) Send(ctx context.Context, text string, onDelta func(string)) (string, error) {
	ch := make(chan string, 64)
	r.eng.StreamCh = ch
	r.eng.InitialMessages = r.msgs

	done := make(chan struct{})
	go func() {
		for d := range ch {
			if onDelta != nil {
				onDelta(d)
			}
		}
		close(done)
	}()

	res, err := r.eng.Run(ctx, text)

	r.eng.StreamCh = nil
	close(ch)
	<-done

	// res.Messages already includes the seeded InitialMessages plus this
	// turn's new messages, so replace rather than append to avoid dupes.
	if len(res.Messages) > 0 {
		r.msgs = res.Messages
	}
	return strings.TrimSpace(res.FinalText), err
}
