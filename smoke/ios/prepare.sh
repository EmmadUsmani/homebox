#!/bin/sh
set -eu
. "$(dirname "$0")/../lib.sh"

if [ "${SMOKE_IOS:-0}" != 1 ]; then
    skip 'iOS Simulator: set SMOKE_IOS=1 to opt in and accept the Xcode license in the guest'
    exit 77
fi
if ! smoke_matches 'iOS Simulator boots and renders the SwiftUI smoke app'; then
    exit 77
fi
if [ "$(uname -s)" != Darwin ] || [ "$(uname -m)" != arm64 ]; then
    skip 'iOS Simulator: requires an Apple silicon Mac host'
    exit 77
fi
guest_os=$("$BOX" ssh "$SMOKE_VM" -- 'uname -s')
if [ "$guest_os" != Darwin ]; then
    skip 'iOS Simulator: requires a macOS guest'
    exit 77
fi

developer=${DEVELOPER_DIR:-$(xcode-select -p 2>/dev/null || true)}
case $developer in
    *.app) developer=$developer/Contents/Developer ;;
esac
xcode=${developer%/Contents/Developer}
if [ ! -x "$developer/usr/bin/xcodebuild" ] || [ "$xcode" = "$developer" ]; then
    skip 'iOS Simulator: select a full Xcode app with xcode-select or DEVELOPER_DIR'
    exit 77
fi
export DEVELOPER_DIR="$developer"
minimum=$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' "$xcode/Contents/Info.plist" 2>/dev/null || true)
guest_version=$("$BOX" ssh "$SMOKE_VM" -- 'sw_vers -productVersion')
sdk=$(xcodebuild -version -sdk iphonesimulator SDKVersion 2>/dev/null || true)
if ! node -e '
    const [guest, minimum, sdk] = process.argv.slice(1);
    const major = version => Number(version.split(".")[0]);
    if (!minimum || guest.localeCompare(minimum, "en", { numeric: true }) < 0 || major(sdk) < 18) process.exit(1);
' "$guest_version" "$minimum" "$sdk"; then
    skip 'iOS Simulator: selected Xcode requires a newer guest or lacks an iOS 18+ Simulator SDK'
    exit 77
fi
runtime=$(xcrun simctl runtime list -j | node -e '
    const fs = require("node:fs");
    const runtimes = Object.values(JSON.parse(fs.readFileSync(0, "utf8")));
    const sdkMajor = process.argv[1].split(".")[0];
    const runtime = runtimes.filter(image =>
        image.platformIdentifier === "com.apple.platform.iphonesimulator" &&
        image.signatureState === "Verified" && image.state === "Ready" &&
        image.version.split(".")[0] === sdkMajor &&
        fs.existsSync(image.path)
    ).sort((a, b) => b.version.localeCompare(a.version, "en", { numeric: true }))[0];
    if (runtime) console.log(runtime.path);
' "$sdk")
if [ -z "$runtime" ]; then
    skip 'iOS Simulator: install a matching iOS Simulator runtime on the host'
    exit 77
fi

cleanup_failed_transfer() {
    "$BOX" ssh "$SMOKE_VM" -- 'rm -rf ~/homebox-smoke/ios/staging' </dev/null || true
}
trap cleanup_failed_transfer EXIT
"$BOX" ssh "$SMOKE_VM" -- 'mkdir -m 700 ~/homebox-smoke/ios/staging'
printf 'Staging Xcode over pinned SSH\n'
bash -o pipefail -c '
    COPYFILE_DISABLE=1 tar --no-xattrs -C "$1" -cf - . |
        "$BOX" ssh "$SMOKE_VM" -- "cd ~/homebox-smoke/ios/staging && mkdir Xcode.app && tar -xf - -C Xcode.app"
' -- "$xcode"
printf 'Staging the installed iOS runtime over pinned SSH\n'
"$BOX" ssh "$SMOKE_VM" -- 'cat > ~/homebox-smoke/ios/staging/runtime.dmg' <"$runtime"
trap - EXIT
