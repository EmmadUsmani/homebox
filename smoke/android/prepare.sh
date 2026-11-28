#!/bin/sh
set -eu
. "$(dirname "$0")/../lib.sh"
if [ "${SMOKE_ANDROID:-0}" != 1 ]; then
    skip 'Android Emulator: set SMOKE_ANDROID=1 to opt in and accept Android SDK licenses'
    exit 77
fi
label='Android Emulator boots with KVM and renders the smoke APK'
if ! smoke_matches "$label"; then
    exit 77
fi
if "$BOX" ssh "$SMOKE_VM" -- 'test -c /dev/kvm && test -r /dev/kvm && test -w /dev/kvm'; then
    exit 0
else
    status=$?
    [ "$status" -eq 1 ] || exit "$status"
    skip 'Android Emulator: /dev/kvm is missing or inaccessible to the guest user'
    exit 77
fi
