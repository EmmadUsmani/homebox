#!/bin/sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=smoke/lib.sh
. "$here/../lib.sh"
artifacts="$SMOKE_ARTIFACTS/gui"
mkdir -p "$artifacts"
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
harness="$here/../../spikes/remote-desktop/harness"
cp "$harness/package.json" "$harness/package-lock.json" "$scratch/"
# Keep the guest evidence and write the gallery even if browser setup or a check fails.
collect_guest_evidence() {
    "$BOX" ssh "$SMOKE_VM" -- 'cd ~/homebox-smoke/artifacts/gui && tar -cf - .' \
        >"$scratch/guest.tar" && tar -xf "$scratch/guest.tar" -C "$artifacts"
}
check 'Collect guest GUI evidence' collect_guest_evidence

if npm ci --prefix "$scratch" --no-audit --no-fund &&
    "$scratch/node_modules/.bin/playwright" install chromium; then
    node "$here/remote.cjs" "$scratch/node_modules" "$BOX" "$SMOKE_VM" "$artifacts" \
        >"$scratch/remote.log" 2>&1 || smoke_failures=$((smoke_failures + 1))
    cat "$scratch/remote.log"
else
    fail 'Prepare the Mac Playwright browser'
    touch "$scratch/remote.log"
fi
check 'Write GUI evidence gallery' node "$here/gallery.cjs" "$artifacts" \
    "$SMOKE_ARTIFACTS/gui-test.log" "$scratch/remote.log"

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
