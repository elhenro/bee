package tui

import "github.com/elhenro/bee/internal/types"

// tutorialStep is one beat of the scripted walkthrough. user (optional) renders
// as a fake user bubble; stream types out as the assistant reply; extra holds
// finished cards revealed after the stream settles (e.g. a tool call + result);
// coach is the overlay guidance for the step.
//
// Every injected message is ephemeral so nonEphemeral() keeps the fake session
// out of the real LLM context — the user's first real turn starts clean.
type tutorialStep struct {
	title  string
	user   string
	stream string
	extra  []types.Message
	coach  string
}

// tutEphemeral builds a scrollback-only message for the walkthrough.
func tutEphemeral(role types.Role, blocks ...types.ContentBlock) types.Message {
	return types.Message{Role: role, Content: blocks, Ephemeral: true}
}

// tutorialSteps returns the scripted first-run tour. Rebuilt per call (cheap);
// covers input, tool calls + approval, roles, and slash commands.
func tutorialSteps() []tutorialStep {
	return []tutorialStep{
		{
			title:  "the input bar",
			stream: "Hi — I'm bee, a coding agent that lives in your terminal. You talk to me in the input bar at the bottom: type a request, press enter to send. shift+enter adds a newline, esc cancels a turn in flight.",
			coach:  "↓ that line at the bottom is the input bar.\nType anything and press enter to send it. esc cancels a running turn.",
		},
		{
			title:  "tool calls & approval",
			user:   "list the go files in this project",
			stream: "On it — I'll search the project with the bash tool.",
			extra: []types.Message{
				tutEphemeral(types.RoleAssistant, types.ContentBlock{
					Type: types.BlockToolUse,
					Use: &types.ToolUse{
						ID:    "tutorial_bash_1",
						Name:  "bash",
						Input: map[string]any{"command": "ls **/*.go | head"},
					},
				}),
				tutEphemeral(types.RoleTool, types.ContentBlock{
					Type: types.BlockToolResult,
					Result: &types.ToolResult{
						UseID:   "tutorial_bash_1",
						Content: "cmd/bee/run.go\ninternal/tui/app.go\ninternal/loop/turn.go\ninternal/config/load.go",
					},
				}),
			},
			coach: "bee uses tools (bash, read, edit, …) to do real work — you just saw a bash call and its result.\nRisky actions pause for approval: enter allows, esc denies. Toggle auto-approve with alt+y.",
		},
		{
			title:  "roles",
			stream: "I run in three roles. worker (default) reads, edits, and runs commands. scout researches read-only and can browse the web. queen splits a big job across a hive of sub-bees.",
			coach:  "shift+tab cycles worker → scout → queen.\nThe active role shows in the top bar. Pick scout for safe research, queen for large multi-step jobs.",
		},
		{
			title:  "slash commands",
			stream: "Type / to open the command palette — /model switches models, /settings toggles the UI, /help lists everything. ctrl+/ cycles caveman brevity. The top bar tracks context fill, model, and cost as you go.",
			coach:  "Press / anytime to see commands.\nThat's the tour — press enter to finish. Replay it later with /tutorial. Happy hacking 🐝",
		},
	}
}
