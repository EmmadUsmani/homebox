#!/usr/bin/env bash
set -euo pipefail
. ./common.sh

sudo timeout 5m apt-get -o Acquire::Retries=2 update
sudo env DEBIAN_FRONTEND=noninteractive timeout 10m apt-get install -y \
    curl unzip openjdk-21-jdk-headless libgl1 libegl1 libnss3 libpulse0 \
    libxcb1 libx11-6 libx11-xcb1 libxkbcommon0 libxkbcommon-x11-0
export JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64

tools="$ANDROID_HOME/cmdline-tools/$tools_version"
mkdir -p "$ANDROID_HOME/cmdline-tools"
if [[ ! -x "$tools/bin/sdkmanager" ]]; then
    work=$(mktemp -d "$ANDROID_HOME/.tools.XXXXXX")
    trap 'rm -rf "$work"' EXIT
    curl --fail --location --retry 2 --max-time 300 -o "$work/tools.zip" \
        https://dl.google.com/android/repository/commandlinetools-linux-14742923_latest.zip
    echo "48833c34b761c10cb20bcd16582129395d121b27  $work/tools.zip" | sha1sum --check
    unzip -q "$work/tools.zip" -d "$work"
    mv "$work/cmdline-tools" "$tools"
fi

# SMOKE_ANDROID=1 explicitly accepts the SDK licenses. sdkmanager closes
# its input when done, so only check sdkmanager's status in this pipeline.
( set +o pipefail; yes | timeout 5m "$tools/bin/sdkmanager" --licenses )
timeout 20m "$tools/bin/sdkmanager" 'platform-tools' 'emulator' \
    'platforms;android-35' "build-tools;${build_tools##*/}" "$image" </dev/null
