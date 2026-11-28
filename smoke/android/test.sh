#!/usr/bin/env bash
set -euo pipefail
. ../lib.sh
. ./common.sh

label='Android Emulator boots with KVM and renders the smoke APK'
smoke_matches "$label" || exit 0
if [[ ! -c /dev/kvm || ! -r /dev/kvm || ! -w /dev/kvm ]]; then
    skip "$label: /dev/kvm is missing or inaccessible to the guest user"
    exit 0
fi

workspace=$(mktemp -d "$PWD/.android.XXXXXX")
emulator_pid=
cleanup() {
    if [[ -n $emulator_pid ]]; then
        kill "$emulator_pid" 2>/dev/null || true
        wait "$emulator_pid" 2>/dev/null || true
    fi
    rm -rf "$workspace"
}
trap cleanup EXIT
export ANDROID_USER_HOME="$workspace/android"
export ANDROID_EMULATOR_HOME="$ANDROID_USER_HOME"
export ANDROID_AVD_HOME="$ANDROID_USER_HOME/avd"
mkdir -p "$ANDROID_AVD_HOME"
export JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64
emulator="$ANDROID_HOME/emulator/emulator"
adb_bin="$ANDROID_HOME/platform-tools/adb"
android_jar="$ANDROID_HOME/platforms/android-35/android.jar"
serial='emulator-5580'
adb() { timeout 25s "$adb_bin" -s "$serial" "$@"; }

rm -f android.png
timeout 30s "$emulator" -accel-check
timeout 25s "$adb_bin" start-server
if timeout 25s "$adb_bin" devices | grep -q "^${serial}[[:space:]]"; then
    fail "$label: ADB emulator port 5580 is in use"
    finish
fi
printf 'no\n' | timeout 60s "$ANDROID_HOME/cmdline-tools/$tools_version/bin/avdmanager" \
    create avd --name homebox-smoke --package "$image"

timeout 60s "$build_tools/aapt2" link -I "$android_jar" --manifest AndroidManifest.xml \
    -o "$workspace/smoke.apk"
timeout 60s "$JAVA_HOME/bin/javac" --release 8 -cp "$android_jar" -d "$workspace" MainActivity.java
timeout 60s "$build_tools/d8" --lib "$android_jar" --min-api 23 --output "$workspace" \
    "$workspace/dev/homebox/smoke/MainActivity.class"
(cd "$workspace" && timeout 60s "$build_tools/aapt" add smoke.apk classes.dex)
timeout 60s "$build_tools/zipalign" -f 4 "$workspace/smoke.apk" "$workspace/aligned.apk"
timeout 60s "$JAVA_HOME/bin/keytool" -genkeypair -keystore "$workspace/smoke.p12" \
    -storepass android -alias smoke -dname 'CN=Homebox smoke' -keyalg RSA -keysize 2048 -validity 30
timeout 60s "$build_tools/apksigner" sign --ks "$workspace/smoke.p12" --ks-pass pass:android \
    --out "$workspace/signed.apk" "$workspace/aligned.apk"

SECONDS=0
# A fresh AVD with snapshots disabled measures a cold boot.
timeout --kill-after=10s 8m "$emulator" -avd homebox-smoke -port 5580 -no-window -no-audio -no-boot-anim \
    -no-snapshot -accel on -gpu swiftshader -memory 3072 -cores 2 >"$workspace/emulator.log" 2>&1 &
emulator_pid=$!
until [[ $(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r' || true) == 1 ]]; do
    if ! kill -0 "$emulator_pid" 2>/dev/null || (( SECONDS >= 300 )); then
        tail -n 40 "$workspace/emulator.log"
        fail "$label: sys.boot_completed did not reach 1"
        finish
    fi
    sleep 2
done
printf 'Android cold boot: %s seconds\n' "$SECONDS"

adb shell input keyevent KEYCODE_WAKEUP
adb shell wm dismiss-keyguard
adb install "$workspace/signed.apk"
launch=$(adb shell am start -W -n dev.homebox.smoke/.MainActivity)
printf '%s\n' "$launch"
if ! grep -q '^Status: ok' <<<"$launch"; then
    fail "$label: activity launch failed"
    finish
fi
# Assert the foreground UI contains the fixture before capturing it.
adb shell uiautomator dump /data/local/tmp/homebox-smoke.xml
adb shell cat /data/local/tmp/homebox-smoke.xml >"$workspace/ui.xml"
grep -q 'text="Homebox Android smoke"' "$workspace/ui.xml"
adb exec-out screencap -p >"$workspace/android.png"
test -s "$workspace/android.png"
mv "$workspace/android.png" android.png
pass "$label"
finish
