#!/bin/sh
# Collect the static site into dist/ for Cloudflare Pages (or any static host).
set -eu

WEB=$(cd "$(dirname "$0")" && pwd)
DIST="$WEB/dist"

rm -rf "$DIST"
mkdir -p "$DIST/assets"
cp -R "$WEB/assets/." "$DIST/assets/"

# Crawler files must sit at the deploy root so CF Pages serves them as real
# files (a static file beats the SPA/index.html catch-all). In assets/ they'd
# only answer /assets/robots.txt, which crawlers never request. 404.html makes
# Pages answer unknown urls with a real 404 instead of the home page.
cp "$WEB/index.html" "$WEB/404.html" "$WEB/favicon.ico" "$WEB/robots.txt" "$WEB/sitemap.xml" "$WEB/llms.txt" "$WEB/_headers" "$DIST/"

echo "🐝 built to $DIST"
