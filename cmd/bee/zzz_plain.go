package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/elhenro/bee/internal/zzz"
)

// driveZzzPlain runs the loop with the single-line status renderer.
func driveZzzPlain(ctx context.Context, stopCh chan struct{}, runner zzz.Runner, zCfg zzz.Config, run *zzz.Run) error {
	status := zzz.NewStatus(os.Stderr)
	var ui zzz.UI = status
	// only attach a stdin reader when stdin is a tty: piped stdin already
	// served as objective input and is closed/empty by now.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		ps := &plainSteerUI{Status: status, steer: make(chan zzz.Steer, 16)}
		go readStdinSteer(ctx, ps)
		ui = ps
		fmt.Fprintln(os.Stderr, "[zzz] plain mode: type /stop, /abort, /note <text>, or free text to nudge.")
	}
	return zzz.Drive(ctx, stopCh, runner, zCfg, run, ui)
}

// plainSteerUI gives plain-mode (non-TUI) operators a steering surface by
// satisfying zzz.Steerable on top of the line-status renderer.
type plainSteerUI struct {
	*zzz.Status
	steer chan zzz.Steer
}

func (p *plainSteerUI) Steer() <-chan zzz.Steer { return p.steer }

func readStdinSteer(ctx context.Context, p *plainSteerUI) {
	r := bufio.NewScanner(os.Stdin)
	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if line == "" {
			continue
		}
		select {
		case p.steer <- parseSteerLine(line):
		case <-ctx.Done():
			return
		}
	}
}

func parseSteerLine(s string) zzz.Steer {
	if !strings.HasPrefix(s, "/") {
		return zzz.Steer{Kind: zzz.SteerNote, Text: s}
	}
	cmd, rest, _ := strings.Cut(s, " ")
	switch strings.ToLower(cmd) {
	case "/stop", "/quit":
		return zzz.Steer{Kind: zzz.SteerStop}
	case "/abort", "/kill":
		return zzz.Steer{Kind: zzz.SteerAbort}
	case "/note", "/say", "/nudge":
		return zzz.Steer{Kind: zzz.SteerNote, Text: strings.TrimSpace(rest)}
	}
	return zzz.Steer{Kind: zzz.SteerNote, Text: s}
}

func hasStdinPipe() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) == 0
}

func readStdinOrEmpty() string {
	if !hasStdinPipe() {
		return ""
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
