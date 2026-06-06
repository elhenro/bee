# tmux-delegated external commands in bee

## Goal

Open vim/neovim, a shell, or any command from bee as an independent tmux
window with a tab bar, while bee keeps generating live in window 0. Toggle
between bee and editor with tmux window switching.

## Core insight

With tmux + windows, bee never suspends. `tmux new-window` returns instantly
and switches focus to the new window; bee keeps running untouched in window 0.
No terminal handoff, no `ExecProcess` race.

## Components

### `internal/mux` — tmux orchestration (pure, no TUI dep)

- `InTmux() bool` — `$TMUX` set.
- `OpenWindow(o Opts) error` — `tmux new-window -n <name> -c <dir> <cmd>`.
  Reuse: if window `<name>` already exists, `select-window` instead of
  spawning a duplicate.
- `OpenEditor(dir, file string, line int) error` — resolve editor, open at
  line via `+<n>`.
- Editor resolution: explicit cfg value → `$VISUAL` → `$EDITOR` → `vim`.

### TUI surface

- `ctrl+e` — open editor on current `cwd` (worktree root).
- `/edit [path[:line]]` — file in editor; no arg = cwd.
- `/term [cmd]` — new window running cmd (any shell command); no arg = `$SHELL`.
- Window 0 renamed `bee` so `prefix+0` always returns.
- Dispatch is a non-blocking `tea.Cmd` running the tmux call; bee stays live.

### Fallback (not inside tmux)

`InTmux()==false` → `tea.ExecProcess` suspend-toggle for a single command +
one-line hint: "tab bar needs tmux; start bee inside tmux."

### Config

Flat field `Editor string` (`toml:"editor"`), default "" (resolve at runtime).

## Out of scope (YAGNI)

No panes, no embedded VT, no @-picker in v1, no injecting tmux/vim keybinds
into user configs.

## Vim ergonomics (documented, not coded)

- bee opens at line via `+N`.
- Hop back from inside vim: `nnoremap <leader>b :silent !tmux select-window -t bee<CR>`.

## Testing

- `mux`: assert generated tmux arg slices; `InTmux` via env injection;
  reuse-vs-new branch.
- TUI: `/edit` + `ctrl+e` dispatch produces the right mux call with cwd;
  fallback path when `$TMUX` empty.
