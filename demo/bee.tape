# bee — basic usage demo: startup, prompt, tool call, response
# render: vhs demo/bee.tape
#
# uses real provider (needs OPENROUTER_API_KEY or your configured backend)
# so tool calls render authentically. for fully offline preview swap to
# `Env BEE_TEST_PROVIDER "stub"` — output will be text-only, no tool chrome.

Output demo/bee.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 14
Set LineHeight 1.3
Set Width 1000
Set Height 600
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set PlaybackSpeed 1.0

Env BEE_NO_FIRSTRUN "1"

Hide
Type "clear" Enter
Sleep 200ms
Show

# startup
Type "bee"
Sleep 400ms
Enter
Sleep 2s

# a prompt that naturally triggers a tool call (read / ls)
Type "list the go files in cmd/bee and tell me what main.go wires up"
Sleep 600ms
Enter

# let the model think, call the tool, stream the answer
# bump if your model is slow on first token
Sleep 30s

# graceful exit
Type "/quit"
Sleep 400ms
Enter
Sleep 1s
