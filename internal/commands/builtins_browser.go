package commands

import (
	"context"
	"fmt"
)

// registerBrowser wires /browser, a live session-only toggle for the native
// browser tools. `/browser on` registers them, `/browser off` removes them,
// bare `/browser` prints usage. not persisted to config.
func registerBrowser(r *Registry) {
	r.Register(Command{
		Name:        "browser",
		Description: "toggle native browser tools for this session (/browser on|off)",
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if len(args) == 0 {
				return "usage: /browser on | /browser off", nil
			}
			switch args[0] {
			case "on", "enable":
				return s.SetBrowserEnabled(true)
			case "off", "disable":
				return s.SetBrowserEnabled(false)
			default:
				return "", fmt.Errorf("usage: /browser on | /browser off")
			}
		},
	})
}
