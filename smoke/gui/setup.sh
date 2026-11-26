#!/bin/sh
set -eu
export DEBIAN_FRONTEND=noninteractive
sudo -n apt-get -o Acquire::Retries=2 update
sudo -n apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends \
    build-essential pkg-config libgtk-4-dev qt6-base-dev python3-tk \
    python3-pyside6.qtwidgets libsdl2-dev default-jdk xdotool x11-apps libxcb-cursor0 \
    imagemagick ffmpeg mesa-utils libgl-dev libegl-dev mesa-vulkan-drivers \
    vulkan-tools libvulkan-dev glslang-tools libcairo2-dev pango1.0-tools \
    fonts-noto-cjk fonts-noto-color-emoji fonts-noto-core pulseaudio pulseaudio-utils
# shellcheck source=smoke/lib.sh
. ../lib.sh
mise="$HOME/.local/bin/mise"
playwright="$HOME/.local/share/homebox/playwright/node_modules/.bin/playwright"
engines=
for engine in chromium firefox webkit; do
    if smoke_matches "Playwright $engine headless renders and responds to clicks" ||
        smoke_matches "Playwright $engine headed renders and responds to clicks"; then
        engines="$engines $engine"
    fi
done
if [ -n "$engines" ]; then
    # shellcheck disable=SC2086 # Split the fixed browser engine names into arguments.
    "$mise" exec -- "$playwright" install-deps $engines
    # shellcheck disable=SC2086
    PLAYWRIGHT_SKIP_BROWSER_GC=1 "$mise" exec -- "$playwright" install $engines
fi
if smoke_matches 'Sandboxed Electron window paints pixels'; then
    electron="$HOME/.cache/homebox-smoke-gui"
    mkdir -p "$electron"
    cp electron-package.json "$electron/package.json"
    "$mise" exec -- npm install --prefix "$electron" --no-audit --no-fund
    test -x "$electron/node_modules/electron/dist/electron" ||
        "$mise" exec -- npm rebuild --prefix "$electron" electron
fi

# A filtered run can skip the desktop startup assertion in test.sh.
if [ -n "${SMOKE_MATCH:-}" ]; then
    timeout 35 /usr/local/lib/homebox-desktop/start
fi
