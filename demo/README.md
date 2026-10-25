# bee demo

`bee.gif` and `screenshots/` are live recordings of the real TUI, not mock-ups. A 27B model (Qwen3.8, 4-bit) runs on a local oMLX server, bee fixes a failing test in a throwaway Go project, and VHS records the terminal at 2x inside the honey frame. Nothing is scripted or sped up.

- `bee.gif`: the README loop, about 25 seconds.
- `screenshots/`: stills from the same sessions (`done`, `work`, `models`, `skills`, `light`).
- The website uses the same renders, cropped to the window: `website/assets/shot-*.webp` and `hero.*`.

The render setup lives in `private/demo-render/` (git-ignored): one VHS tape per scene, `render.sh` to record them, `post.sh` to turn the raw frames into the gif, the README PNGs and the website media. It needs `vhs`, `ffmpeg`, `gifsicle`, `librsvg`, `imagemagick` and the JetBrainsMono Nerd Font, plus a running `omlx serve --headless --port 8080`.

A real model is not deterministic. If a take wanders (it can), run the scene again.

## Quick take

`bee.tape` records a short session with whatever provider your config points at, to `bee-quick.gif`:

```sh
brew install vhs ttyd ffmpeg gifsicle                   # one-time
brew install --cask font-jetbrains-mono-nerd-font       # one-time
vhs demo/bee.tape                                       # -> demo/bee-quick.gif
gifsicle -O3 --lossy=80 --colors 128 demo/bee-quick.gif -o demo/bee-quick.gif
```

Keep a gif under about 2 MB so the README scrolls smoothly. Flat dark UI like bee's compresses well.
