package commands

import (
	"context"
	"errors"
)

const browserUsage = "usage: /browser on | /browser off"

// registerBrowser wires /browser, a live session-only toggle for the native
// browser tools. `/browser on` registers them, `/browser off` removes them,
// bare `/browser` prints usage. not persisted to config.
func registerBrowser(r *Registry) {
	r.Register(Command{
		Name:        "browser",
		Description: "toggle native browser tools for this session (/browser on|off)",
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if len(args) == 0 {
				return browserUsage, nil
			}
			on := args[0] == "on" || args[0] == "enable"
			if !on && args[0] != "off" && args[0] != "disable" {
				return "", errors.New(browserUsage)
			}
			if s == nil {
				return "", nil
			}
			return s.SetBrowserEnabled(on)
		},
	})
}
