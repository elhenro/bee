package commands

// registerTUIOnly adds commands the TUI special-cases itself (app_slash.go):
// registration gives them palette discovery, and most fire mid-turn. Run is
// the generic-dispatch fallback for contexts without the TUI.
func registerTUIOnly(r *Registry) {
	external := "external commands run in the TUI only"
	r.Register(
		// the interactive loop lives in handleGoal
		textCmd("goal", "set a completion goal; loop turns until a fast model says it's met", true,
			"usage: /goal <condition> | /goal show | /goal clear"),
		textCmd("remote-control", "serve a local web relay (URL+QR) to drive bee from a phone/browser", true,
			"run `bee remote-control` in a separate terminal to start the local web relay; it prints a URL + QR to open on another device."),
		// cancels the in-flight turn, same as pressing esc while generating
		textCmd("stop", "cancel the running turn (same as esc)", true, "nothing to stop"),
		// flips the session-only auto-resume toggle on the Model
		textCmd("watchdog", "toggle auto-resume on stall/error (on|off)", true, "watchdog toggle is TUI-only"),
		textCmd("agents", "parallel-agents overview — open in a new shell: bee agents", true,
			"Quit (ctrl+d ctrl+d) and run `bee agents` to open the parallel-agents overview. Each chat message there spawns a new agent in its own git worktree."),
		// launch a tmux window or suspend-and-run, so the editor opens while bee is still generating
		textCmd("edit", "open file in editor (tmux window): /edit [path[:line]]", true, external),
		textCmd("vim", "open editor on cwd or file: /vim [path[:line]]", true, external),
		textCmd("term", "run a command in a tmux window: /term [cmd]", true, external),
		// opens the interactive walkthrough overlay
		openPane("tutorial", "replay the interactive walkthrough", false, Side.OpenTutorial,
			"run `bee` in an interactive terminal to start the tutorial."),
		// pick a bigger model, then hand it a distilled brief of the stuck
		// session. usable mid-stream: committing a pick cancels the stuck turn
		openPane("handoff", "hand the stuck session to a bigger model you pick", true, Side.OpenHandoff,
			"handoff needs the interactive picker; use `/model <provider>/<id>` then resend your task"),
	)
}
