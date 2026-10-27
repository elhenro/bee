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

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/remote"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// runRemoteControl serves a local web relay so another device on the LAN can
// drive this bee session. Reuses the headless engine construction.
func runRemoteControl(args []string) {
	fs := flag.NewFlagSet("remote-control", flag.ContinueOnError)
	port := fs.Int("port", 0, "listen port (0 = OS-assigned)")
	lan := fs.Bool("lan", false, "bind all interfaces to expose on the LAN (default: loopback only). The bearer token rides in a cleartext-HTTP URL, so only expose on a trusted network.")
	yes := fs.Bool("yes", false, "acknowledge that clients can run tools locally on this machine (does not auto-approve commands)")
	yolo := fs.Bool("yolo", false, "auto-approve dangerous shell commands from clients; without it they are asked on this terminal, or denied when stdin is not one")
	var ef engineFlags
	ef.register(fs)
	parseFlags(fs, args)

	cfg := ef.mustLoad("bee remote-control", "")

	// trust gate: anyone on the LAN who reaches the URL can drive the agent
	// and run tools locally. Refuse full-access without explicit --yes.
	scope := cfg.Sandbox.Scope
	if scope == "danger-full-access" && !*yes {
		fmt.Fprintln(os.Stderr, "bee remote-control: refusing to start with sandbox scope danger-full-access.")
		fmt.Fprintln(os.Stderr, "anyone on your LAN who opens the URL can run tools on this machine with full access.")
		exitf(2, "to proceed: re-run with --yes, or pick a tighter scope with --sandbox workspace-write.")
	}
	fmt.Fprintf(os.Stdout, "remote-control: clients execute locally under sandbox scope %s\n", scope)
	if *yolo || cfg.Yolo {
		fmt.Fprintln(os.Stdout, "remote-control: dangerous commands are auto-approved")
	}

	prov := mustProvider("bee remote-control", cfg)

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	app := headlessApprover(cfg, *yolo, "--yolo")
	if c, ok := app.(*approval.Cache); ok {
		defer c.Flush()
	}
	reg, err := buildToolsWithApprover(cwd, cfg, prov, storeDir, app)
	if err != nil {
		exitf(1, "bee remote-control: tools: %v", err)
	}

	ensureFirstRun()
	skillReg := newSkillRegistry()
	registerSkillTools(reg, skillReg, cfg, cwd)
	roll, err := session.Open(uuid.NewString())
	if err != nil {
		exitf(1, "bee remote-control: session: %v", err)
	}
	defer roll.Close()

	eng := newHeadlessEngine(prov, reg, skillReg, cfg, cwd, roll, os.Stdout)

	adapter := &remoteEngine{eng: eng}
	// bind loopback by default — the capability token travels in a cleartext
	// HTTP URL, so an all-interfaces bind exposes it to anyone on the LAN.
	// --lan opts into the wider bind.
	bindHost := "127.0.0.1"
	if *lan {
		bindHost = ""
	}
	srv := remote.New(adapter, remote.Options{Addr: fmt.Sprintf("%s:%d", bindHost, *port), Title: "bee", Log: os.Stdout})
	ln, url, err := srv.Start()
	if err != nil {
		exitf(1, "bee remote-control: start: %v", err)
	}

	if *lan {
		// LAN bind: the URL carries a routable host, so another device can reach
		// it — print it with a scannable QR.
		fmt.Fprintf(os.Stdout, "\n  open from another device:\n  %s\n\n", url)
		if qr, err := remote.RenderQR(url); err == nil {
			fmt.Fprint(os.Stdout, qr)
			fmt.Fprintln(os.Stdout)
		}
	} else {
		// loopback bind (default): the URL is local-only, so don't claim it works
		// from another device or render a useless QR. Point at --lan instead.
		fmt.Fprintf(os.Stdout, "\n  local URL (loopback only):\n  %s\n\n", url)
		fmt.Fprintln(os.Stdout, "  to drive this from another device (phone, tablet), re-run with --lan.")
		fmt.Fprintln(os.Stdout, "  the access token rides in a cleartext HTTP URL — only expose on a trusted network.")
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
