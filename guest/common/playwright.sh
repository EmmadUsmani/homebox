#!/bin/sh
set -eu

workspace="$HOME/.local/share/homebox/playwright"
mise="$HOME/.local/bin/mise"
mkdir -p "$workspace"
if [ ! -f "$workspace/package-lock.json" ]; then
    "$mise" exec -- npm install --prefix "$workspace" --save-exact playwright
else
    "$mise" exec -- npm ci --prefix "$workspace"
fi
"$mise" exec -- "$workspace/node_modules/.bin/playwright" install chromium
