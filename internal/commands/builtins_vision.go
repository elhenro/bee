package commands

import "context"

// registerVision wires /vision, a session-only setter for the fallback vision
// model. bee routes images through it when the main model can't see. Bare
// /vision reports status; `/vision <model> [endpoint] [api]` sets it.
func registerVision(r *Registry) {
	r.Register(Command{
		Name:        "vision",
		Description: "set fallback vision model for non-vision models (/vision <model> [endpoint] [api])",
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "usage: /vision <model> [endpoint] [api]", nil
			}
			var model, endpoint, api string
			if len(args) > 0 {
				model = args[0]
			}
			if len(args) > 1 {
				endpoint = args[1]
			}
			if len(args) > 2 {
				api = args[2]
			}
			return s.VisionFallback(model, endpoint, api)
		},
	})
}
