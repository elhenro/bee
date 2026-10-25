# bee website

The landing page at https://bee.hnr.bz. A static page (HTML, two stylesheets, one script) plus a tiny Go server that embeds it for local work. No build step, no framework.

## Run locally

```sh
go run .
# http://localhost:8080  (PORT=9000 go run . to change it)
```

The server embeds `index.html`, `assets/`, `robots.txt`, `sitemap.xml` and `llms.txt` at compile time, so restart it after an edit.

## Build and deploy

```sh
./build.sh    # collects the static site into dist/
./deploy.sh   # build.sh, then wrangler pages deploy dist (Cloudflare Pages)
```

`dist/` is plain static files. `_headers` sets the cache and security headers on Cloudflare Pages. Crawler files sit at the deploy root so they answer `/robots.txt` and friends.

## Layout

```
index.html           the page, plus JSON-LD (WebSite, SoftwareApplication, FAQPage)
assets/site.css      tokens, base, nav, hero
assets/sections.css  the page sections
assets/site.js       theme, copy buttons, install tabs, gallery, hero video
assets/fonts/        Fraunces and JetBrains Mono (OFL, licenses next to the files)
assets/shot-*.webp   the terminal windows, cropped from the demo renders
assets/hero.*        the hero loop (webm and mp4)
404.html             the not found page (Pages serves it with a real 404)
favicon.ico          for crawlers and browsers that ask for /favicon.ico
main.go              the local server
```

## Design

- Honey on cream, ink on near-black in dark mode, one accent (`#ffb000`, the same honey the TUI uses). Hex shapes are the motif: the bullets, the stage behind every screenshot, the backdrop of the closing band.
- Serif headings (Fraunces), system sans for text, JetBrains Mono for anything typed in a terminal.
- Theme follows the system until the toggle is used, then the choice is stored in `localStorage` (`bee-theme`).
- The FAQ is native `<details>`. Its text must match the FAQPage JSON-LD at the bottom of `index.html`.

## Behavior worth knowing

- The hero loop plays only while visible, never under `prefers-reduced-motion`, and can always be paused. Without scripts it falls back to the native video controls.
- The screenshot gallery is a tab list: arrow keys, Home and End move between tabs, swipe works on touch, and a click opens the full size image. Without scripts all screenshots stack.
- On a phone the full size view scrolls, because a terminal is not readable at 390px.

## Refreshing the screenshots

The windows are cropped out of the demo renders (`demo/screenshots/*.png` are the framed versions of the same runs). After a new render, crop the 2x frame at 81px inset, round the corners and encode with `cwebp -q 88 -alpha_q 100`.
