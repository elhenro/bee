# bee website

A single Go binary that serves the bee landing page. Zero dependencies at runtime.

## Run locally

```sh
# one-liner
go run .

# or build and run
go build -o website .
./website
# http://localhost:8080
```

The binary embeds all assets (images, favicon, robots.txt, sitemap.xml) via `go:embed`. No external files needed.

## Build for production

```sh
./build.sh
# built to dist/
#   dist/website          — the binary
#   dist/index.html       — extracted HTML for static hosting
#   dist/assets/*         — static assets
```

The `build.sh` script builds the binary, runs it briefly, extracts the HTML for static hosting (Cloudflare Pages, GitHub Pages, etc.), and copies assets into `dist/`.

## What it serves

- `/` — the landing page (HTML with inline CSS/JS)
- `/assets/*` — images, favicon, og-image
- `/robots.txt` — crawl rules
- `/sitemap.xml` — sitemap

## How it works

```
main.go
  ├── embed.FS (assets/*)
  ├── indexHTML (string literal — the whole page)
  └── chi router
        GET /          → serve indexHTML
        GET /assets/*  → serve from embed.FS
        GET /robots.txt
        GET /sitemap.xml
```

The HTML is a string literal in `main.go` — no template files, no template compilation. Just `w.Write([]byte(indexHTML))`.

Assets (screenshots, favicon, og-image) are embedded at compile time. The binary is fully self-contained.

## Features

- Dark/light theme toggle (persists in localStorage)
- Screenshot gallery with auto-rotate, keyboard nav, touch swipe
- Copy-on-click install commands
- Schema.org structured data
- Responsive (mobile-friendly)
- Theme-color meta tag for mobile browsers

## Config

| Env var | Default | What it does |
|---|---|---|
| `PORT` | `8080` | Listen port |

## Architecture

```
website/
  main.go          — server + embedded HTML
  assets/          — images, favicon, robots.txt, sitemap
  build.sh         — production build script
  go.mod           — single dep: chi v5
```

Single dependency: `github.com/go-chi/chi/v5`. Everything else is stdlib.