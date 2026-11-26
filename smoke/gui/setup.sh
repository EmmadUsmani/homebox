#!/bin/sh
set -eu
export DEBIAN_FRONTEND=noninteractive
set -- libgtk-4-dev xdotool x11-apps mesa-utils libgl-dev libegl-dev mesa-vulkan-drivers \
    libvulkan-dev glslang-tools libcairo2-dev pango1.0-tools \
    fonts-noto-cjk fonts-noto-color-emoji fonts-noto-core pulseaudio pulseaudio-utils
if [ "${SMOKE_LEVEL:-full}" = full ]; then
    set -- "$@" qt6-base-dev python3-tk python3-pyside6.qtwidgets libsdl2-dev default-jdk \
        libxcb-cursor0 vulkan-tools
fi
sudo -n apt-get -o Acquire::Retries=2 update
sudo -n apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends "$@"
# shellcheck source=smoke/lib.sh
. ../lib.sh
mise="$HOME/.local/bin/mise"
playwright="$HOME/.local/share/homebox/playwright/node_modules/.bin/playwright"
engines=
if [ "${SMOKE_LEVEL:-full}" = full ]; then
    set -- chromium firefox webkit
else
    # The Ubuntu profile already installs Chromium and its dependencies.
    set --
fi
for engine do
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
if [ "${SMOKE_LEVEL:-full}" = full ] && smoke_matches 'Sandboxed Electron window paints pixels'; then
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
