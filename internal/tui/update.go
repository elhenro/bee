package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/update"
)

// Commit is the build-time short sha of the running binary. cmd/bee sets it
// at process start; "" or "dev" disables the upstream probe (developer
// builds shouldn't auto-prompt against main).
var Commit = ""

// updateCheckConfig is what the goroutine needs to do its job. Constructed
// from cfg.UpdateCheck / cfg.UpdateRepo / cfg.UpdateBranch in app_run.
//
// mode is a func instead of a string so the goroutine sees live changes —
// e.g. user picks "always auto" or "never" mid-session and the next probe
// honors the new policy without restarting bee.
type updateCheckConfig struct {
	mode     func() string // returns "ask" | "auto" | "off"
	repo     string
	branch   string
	interval time.Duration
}

// updateAvailableMsg surfaces a probed update into the bubbletea loop.
type updateAvailableMsg struct{ Info update.Info }

// updateAppliedMsg fires when the install subprocess finishes.
type updateAppliedMsg struct {
	output string
	err    error
}

// startUpdateChecker spawns a hourly probe goroutine. Exits on ctx.Done.
// First probe happens after a short warmup so it doesn't compete with intro
// frames + the live model-catalogue pre-warm.
func startUpdateChecker(ctx context.Context, p *tea.Program, cfg updateCheckConfig) {
	if Commit == "" || Commit == "dev" {
		return
	}
	if cfg.mode == nil {
		return
	}
	if cfg.interval <= 0 {
		cfg.interval = time.Hour
	}
	go func() {
		// initial warmup — give the TUI room to settle before the first probe.
		select {
		case <-time.After(15 * time.Second):
		case <-ctx.Done():
			return
		}
		t := time.NewTicker(cfg.interval)
		defer t.Stop()
		probeOnce(ctx, p, cfg)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				probeOnce(ctx, p, cfg)
			}
		}
	}()
}

func probeOnce(ctx context.Context, p *tea.Program, cfg updateCheckConfig) {
	mode := cfg.mode()
	if mode == "off" {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := update.Probe(pctx, cfg.repo, cfg.branch, Commit)
	if err != nil || !info.Available() {
		return
	}
	if mode == "auto" {
		applyAndNotify(ctx, p, info)
		return
	}
	p.Send(updateAvailableMsg{Info: info})
}

// applyAndNotify runs the installer and reports the outcome through the same
// updateAppliedMsg path as the manual "update now" button. Used only when
// the user has opted into "always auto" — surfacing a "updating bee…" notice
// first so the silent install doesn't feel like a black hole.
func applyAndNotify(ctx context.Context, p *tea.Program, info update.Info) {
	p.Send(warningMsg{Text: "updating bee… (" + shortSHA(info.LatestSHA) + ")"})
	actx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := update.Apply(actx)
	p.Send(updateAppliedMsg{output: string(out), err: err})
}

// summarizeApplyOutput keeps the warn line short — full output would shove
// the chrome aside. Falls back to last non-empty line on no clear signal.
func summarizeApplyOutput(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "bee-install: done") {
			return l
		}
	}
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" {
			return l
		}
	}
	return ""
}
