# shellcheck shell=bash
# shellcheck disable=SC2034 # shared values consumed by setup.sh and test.sh
export ANDROID_HOME="$HOME/.cache/homebox-smoke/android-sdk"
image='system-images;android-35;default;x86_64'
tools_version=20.0
build_tools="$ANDROID_HOME/build-tools/35.0.0"
