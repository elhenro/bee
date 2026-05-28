package commands

import "context"

// registerGoal wires the /goal fallback. The real interactive behavior is
// special-cased in the TUI (app_slash.go -> handleGoal); this Run is only the
// generic-dispatch fallback and returns a short usage string.
func registerGoal(r *Registry) {
	r.Register(Command{
		Name:           "goal",
		Description:    "set a completion goal; loop turns until a fast model says it's met",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "usage: /goal <condition> | /goal show | /goal clear", nil
		},
	})
}
