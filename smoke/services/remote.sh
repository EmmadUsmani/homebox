#!/bin/sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=smoke/lib.sh
. "$here/../lib.sh"

if ! command -v node >/dev/null || ! node -e 'process.exit(typeof WebSocket === "function" ? 0 : 1)'; then
    skip 'Laptop LAN checks require Node.js 22 or later'
    exit 0
fi
scratch=$(mktemp -d)
trap 'node -e '\''require("node:fs").rmSync(process.argv[1], {recursive: true})'\'' "$scratch"' EXIT
harness="$here/../../spikes/remote-desktop/harness"
cp "$harness/package.json" "$harness/package-lock.json" "$scratch/"
if npm ci --prefix "$scratch" --no-audit --no-fund &&
    "$scratch/node_modules/.bin/playwright" install chromium; then
    export SMOKE_PLAYWRIGHT="$scratch/node_modules/playwright"
else
    fail 'Prepare laptop Chromium'
fi
check_status=0
node "$here/lan.cjs" "$scratch" || check_status=$?
finish
exit "$check_status"
