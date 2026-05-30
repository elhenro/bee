package commands

import "context"

// registerStop wires /stop. The TUI special-cases it (app_slash.go) to cancel
// the in-flight turn — same as pressing esc while generating. This Run is the
// generic-dispatch fallback for contexts with no live turn to cancel.
func registerStop(r *Registry) {
	r.Register(Command{
		Name:           "stop",
		Description:    "cancel the running turn (same as esc)",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "nothing to stop", nil
		},
	})
}
