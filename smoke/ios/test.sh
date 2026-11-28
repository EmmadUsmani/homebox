#!/bin/bash
set -euo pipefail
. ../lib.sh

label='iOS Simulator boots and renders the SwiftUI smoke app'
smoke_matches "$label" || exit 0
export DEVELOPER_DIR="$PWD/staging/Xcode.app/Contents/Developer"
runtime=
device=
bundle_id=dev.homebox.ios-smoke
bounded() { /usr/bin/perl -e 'alarm shift; exec @ARGV or die "exec: $!"' "$@"; }
sim() { bounded 45 xcrun simctl "$@"; }

cleanup() {
    status=$?
    if [ -n "$device" ]; then
        sim shutdown "$device" || true
        sim delete "$device" || status=1
    fi
    if [ -n "$runtime" ]; then
        sim runtime delete "$runtime" || status=1
    fi
    rm -rf staging
    exit "$status"
}
trap cleanup EXIT

bounded 120 sudo -n env "DEVELOPER_DIR=$DEVELOPER_DIR" xcodebuild -license accept
bounded 300 sudo -n env "DEVELOPER_DIR=$DEVELOPER_DIR" xcodebuild -runFirstLaunch
app=staging/IOSSmoke.app
mkdir "$app"
cp Info.plist "$app/Info.plist"
bounded 120 xcrun --sdk iphonesimulator swiftc -parse-as-library -target arm64-apple-ios18.0-simulator \
    SmokeApp.swift -o "$app/IOSSmoke"
codesign --sign - "$app"

runtime=$(sim runtime add "$PWD/staging/runtime.dmg" --move --async)
[[ $runtime =~ ^[[:xdigit:]-]{36}$ ]]
for ((attempt=0; attempt<90; attempt++)); do
    state=$(sim runtime list -j | plutil -extract "$runtime.state" raw -o - - 2>/dev/null || true)
    [ "$state" = Ready ] && break
    sleep 2
done
if [ "$state" != Ready ]; then
    sim runtime list || true
    fail 'Imported iOS Simulator runtime did not become ready'
    finish
fi
runtime_id=$(sim runtime list -j | plutil -extract "$runtime.runtimeIdentifier" raw -o - -)
device=$(sim create HomeboxSmoke com.apple.CoreSimulator.SimDeviceType.iPhone-16 "$runtime_id")
bounded 180 xcrun simctl bootstatus "$device" -b
sim install "$device" "$app"
app_log="$PWD/staging/app.log"
sim launch --stdout="$app_log" "$device" "$bundle_id"

for ((attempt=0; attempt<30; attempt++)); do
    if [ -f "$app_log" ] && grep -Fxq 'Homebox iOS smoke' "$app_log"; then
        sim io "$device" screenshot screenshot.png
        pass "$label"
        exit 0
    fi
    sleep 2
done
fail 'SwiftUI did not log its visible text'
finish
