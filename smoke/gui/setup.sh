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
mise="$HOME/.local/bin/mise"
playwright="$HOME/.local/share/homebox/playwright/node_modules/.bin/playwright"
"$mise" exec -- "$playwright" install-deps chromium firefox webkit
PLAYWRIGHT_SKIP_BROWSER_GC=1 "$mise" exec -- "$playwright" install chromium firefox webkit
electron="$HOME/.cache/homebox-smoke-gui"
mkdir -p "$electron"
cp electron-package.json "$electron/package.json"
"$mise" exec -- npm install --prefix "$electron" --no-audit --no-fund
test -x "$electron/node_modules/electron/dist/electron" ||
    "$mise" exec -- npm rebuild --prefix "$electron" electron
