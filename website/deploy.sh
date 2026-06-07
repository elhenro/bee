#!/bin/sh
# Build the website and deploy dist/ to Cloudflare Pages.
# Auth lives in your local wrangler login (OAuth), not in this repo,
# so this script is safe to commit publicly.
set -eu

WEB=$(cd "$(dirname "$0")" && pwd)
PROJECT=bee
BRANCH=main

# build dist/ (binary + extracted index.html + assets)
sh "$WEB/build.sh"

# deploy to Cloudflare Pages
wrangler pages deploy "$WEB/dist" \
  --project-name="$PROJECT" \
  --branch="$BRANCH" \
  --commit-dirty=true

echo "🐝 deployed to https://bee.hnr.bz"
