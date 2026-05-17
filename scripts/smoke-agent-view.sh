#!/bin/sh
# scripts/smoke-agent-view.sh
# Spawns two background bees, then verifies status sidecars land in
# $BEE_HOME/sessions/bg/. Uses the stub provider so no network is needed.
set -eu
export BEE_TEST_PROVIDER=stub BEE_HOME=$(mktemp -d)
BEE=${BEE:-./bee}

"$BEE" bg "task A" >/tmp/bg-a.out
"$BEE" bg "task B" >/tmp/bg-b.out
sleep 2

# at least two .status.json sidecars should exist
count=$(ls "$BEE_HOME/sessions/bg/"*.status.json 2>/dev/null | wc -l | tr -d ' ')
if [ "$count" -lt 2 ]; then
  echo "FAIL: expected >=2 status sidecars, got $count"
  ls -la "$BEE_HOME/sessions/bg/" || true
  exit 1
fi

# the awaiting/active/idle/done state should be one of the known values
for f in "$BEE_HOME/sessions/bg/"*.status.json; do
  state=$(grep -o '"state": "[a-z]*"' "$f" | head -1)
  case "$state" in
    *active*|*awaiting*|*idle*|*done*|*failed*) ;;
    *) echo "FAIL: bad state in $f: $state"; cat "$f"; exit 1 ;;
  esac
done

# cleanup spawned bg processes
for f in "$BEE_HOME/sessions/bg/"*.status.json; do
  id=$(basename "$f" .status.json)
  "$BEE" bg --kill "$id" >/dev/null 2>&1 || true
done

echo "OK"
