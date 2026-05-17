# bee demo tape

VHS tape for the README gif. Re-render with:

```sh
brew install vhs ttyd ffmpeg gifsicle                   # one-time
brew install --cask font-jetbrains-mono-nerd-font       # one-time
vhs demo/bee.tape                                       # → demo/bee.gif (~4 MB raw)
gifsicle -O3 --lossy=80 --colors 128 demo/bee.gif -o demo/bee.gif   # → ~1.7 MB
```

## Conventions

- 1000x600, JetBrainsMono Nerd Font 14, Catppuccin Mocha — match bee TUI palette
- Real provider used so tool calls render authentically (set `OPENROUTER_API_KEY` first, or whatever your config points at). Swap to `Env BEE_TEST_PROVIDER "stub"` for offline preview — output will be text-only, no tool chrome.
- Keep gif under ~2 MB for smooth README scroll. `gifsicle -O3 --lossy=80 --colors 128` is the sweet spot for bee's flat dark TUI.
