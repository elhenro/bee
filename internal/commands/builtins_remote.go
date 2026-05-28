package commands

import "context"

// registerRemoteControl wires the /remote-control fallback. The TUI special-
// cases it (app_slash.go) to print guidance; this Run is the generic-dispatch
// fallback telling the user to start the relay from a terminal.
func registerRemoteControl(r *Registry) {
	r.Register(Command{
		Name:           "remote-control",
		Description:    "serve a local web relay (URL+QR) to drive bee from a phone/browser",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "run `bee remote-control` in a separate terminal to start the local web relay; it prints a URL + QR to open on another device.", nil
		},
	})
}
