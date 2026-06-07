#!/bin/sh
# Build website binary + assets into dist/
set -eu

WEB=$(cd "$(dirname "$0")" && pwd)
DIST="$WEB/dist"

rm -rf "$DIST"
mkdir -p "$DIST/assets"
cp "$WEB/assets"/* "$DIST/assets/"

# Crawler files must sit at the deploy root so CF Pages serves them as real
# files (a static file beats the SPA/index.html catch-all). In assets/ they'd
# only answer /assets/robots.txt, which crawlers never request.
cp "$WEB/robots.txt" "$WEB/sitemap.xml" "$WEB/llms.txt" "$DIST/"

# Build the binary
cd "$WEB"
GOBIN="$DIST" go build -o "$DIST/website" .

# Extract index.html from the binary for static hosting (CF Pages, etc.)
# Run briefly, grab the HTML, then stop
"$DIST/website" > /tmp/bee-w.log 2>&1 &
PID=$!
sleep 1
curl -s http://localhost:8080 > "$DIST/index.html"
kill $PID 2>/dev/null || true

echo "🐝 built to $DIST"
echo "  run: $DIST/website"