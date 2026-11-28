#!/bin/sh
set -eu
if [ -f "$SMOKE_ARTIFACTS/android-test.log" ] && grep -q '^PASS Android Emulator boots with KVM' "$SMOKE_ARTIFACTS/android-test.log"; then
    "$BOX" ssh "$SMOKE_VM" -- 'cat ~/homebox-smoke/android/android.png' \
        >"$SMOKE_ARTIFACTS/android.png.part"
    mv "$SMOKE_ARTIFACTS/android.png.part" "$SMOKE_ARTIFACTS/android.png"
fi
