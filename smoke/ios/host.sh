#!/bin/sh
set -eu

artifact=$SMOKE_ARTIFACTS/ios-simulator.png
if "$BOX" ssh "$SMOKE_VM" -- 'test -s ~/homebox-smoke/ios/screenshot.png'; then
    trap 'rm -f "$artifact.partial"' EXIT
    "$BOX" ssh "$SMOKE_VM" -- 'cat ~/homebox-smoke/ios/screenshot.png' >"$artifact.partial"
    mv "$artifact.partial" "$artifact"
    printf 'Screenshot: %s\n' "$artifact"
else
    status=$?
    [ "$status" = 1 ] || exit "$status"
fi
