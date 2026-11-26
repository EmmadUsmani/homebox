#!/bin/sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=smoke/lib.sh
. "$here/../lib.sh"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
harness="$here/../../spikes/remote-desktop/harness"
cp "$harness/package.json" "$harness/package-lock.json" "$scratch/"
npm ci --prefix "$scratch" --no-audit --no-fund
"$scratch/node_modules/.bin/playwright" install chromium
node "$here/remote.cjs" "$scratch/node_modules" "$BOX" "$SMOKE_VM" || smoke_failures=$((smoke_failures + 1))

"$BOX" ssh-config --include >"$scratch/ssh-config"
if DISPLAY=:99 /usr/bin/ssh -F "$scratch/ssh-config" -Y -o BatchMode=yes -o ConnectTimeout=5 \
    "$SMOKE_VM" 'test -z "${DISPLAY:-}" && echo display-unset' >"$scratch/ssh-out" 2>"$scratch/ssh-error" &&
    grep -qx display-unset "$scratch/ssh-out" &&
    grep -qi 'X11 forwarding request failed' "$scratch/ssh-error"; then
    pass 'SSH refuses an X11 forwarding request'
else
    fail 'SSH refuses an X11 forwarding request'
fi
finish
