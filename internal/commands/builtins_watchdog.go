package commands

import "context"

// registerWatchdog wires /watchdog [on|off]. The TUI special-cases it
// (app_slash.go) to flip the session-only auto-resume toggle on the Model.
// This Run is the generic-dispatch fallback for non-TUI contexts.
func registerWatchdog(r *Registry) {
	r.Register(Command{
		Name:           "watchdog",
		Description:    "toggle auto-resume on stall/error (on|off)",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "watchdog toggle is TUI-only", nil
		},
	})
}
