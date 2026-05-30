---
name: browse
type: recipe
description: Open a URL, snapshot the page, and report console errors. Use to check a running website or game.
steps:
  - id: open
    description: open the target URL and capture the accessibility snapshot
    tool: browser_open
  - id: console
    description: read console output and surface any errors or warnings
    tool: browser_console
---

Open the page the user named, read the snapshot to understand the layout and
interactive elements (each has a `[ref]`), then check the console for errors.
Report what you see and what (if anything) is broken. To interact, call
`browser_click` / `browser_type` with a ref from the snapshot.
