package commands

import "context"

// registerHandoff adds /handoff: pick a bigger model, then hand it a distilled
// brief of the stuck session so it can take over and finish.
func registerHandoff(r *Registry) {
	r.Register(Command{
		Name:        "handoff",
		Description: "hand the stuck session to a bigger model you pick",
		// usable mid-stream: the picker overlays the running turn; committing a
		// pick cancels the stuck turn before the handoff.
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			if err := s.OpenHandoff(); err != nil {
				// headless / no picker: can't choose interactively.
				return "handoff needs the interactive picker; use `/model <provider>/<id>` then resend your task", nil
			}
			return "", nil // side effect: dispatch opens the picker
		},
	})
}
