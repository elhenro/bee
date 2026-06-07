package commands

import "context"

// registerTutorial adds /tutorial, which replays the interactive first-run
// walkthrough. The TUI side opens the overlay; headless returns the error.
func registerTutorial(r *Registry) {
	r.Register(Command{
		Name:        "tutorial",
		Description: "replay the interactive walkthrough",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			if err := s.OpenTutorial(); err != nil {
				return "run `bee` in an interactive terminal to start the tutorial.", nil
			}
			return "", nil
		},
	})
}
