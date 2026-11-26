#!/bin/bash
# shellcheck disable=SC2016
set -eu
# check_sh evaluates its snippet in a separate shell.
# shellcheck source=smoke/lib.sh
. ../lib.sh
# shellcheck source=smoke/gui/session.sh
. ./session.sh
scratch=$(mktemp -d)
export scratch
trap 'rm -rf "$scratch"' EXIT
export GDK_BACKEND=x11 QT_QPA_PLATFORM=xcb LIBGL_ALWAYS_SOFTWARE=1
mise="$HOME/.local/bin/mise"
playwright="$HOME/.local/share/homebox/playwright/node_modules/playwright"

check 'Start the installed KasmVNC desktop' timeout 35 /usr/local/lib/homebox-desktop/start
check 'KasmVNC X display' timeout 5 xdpyinfo
check_sh 'GTK4 C window paints pixels' 'cc gtk.c $(pkg-config --cflags --libs gtk4) -o "$scratch/gtk" &&
    timeout 30 ./window.sh SmokeGTK "$scratch/gtk"'
check_sh 'Qt 6 C++ window paints pixels' 'c++ qt.cpp $(pkg-config --cflags --libs Qt6Widgets) -o "$scratch/qt" &&
    timeout 30 ./window.sh SmokeQt "$scratch/qt"'
check 'Tkinter window paints pixels' timeout 30 ./window.sh SmokeTk /usr/bin/python3 window.py tk
check 'PySide6 window paints pixels' timeout 30 ./window.sh SmokePySide /usr/bin/python3 window.py pyside
check_sh 'SDL2 window paints pixels' 'cc sdl.c $(pkg-config --cflags --libs sdl2) -o "$scratch/sdl" &&
    timeout 30 ./window.sh SmokeSDL "$scratch/sdl"'
check_sh 'Swing window paints pixels' 'javac -d "$scratch" SmokeSwing.java &&
    timeout 30 ./window.sh SmokeSwing java -cp "$scratch" SmokeSwing'
check 'Sandboxed Electron window paints pixels' timeout 45 ./window.sh SmokeElectron \
    "$HOME/.cache/homebox-smoke-gui/node_modules/.bin/electron" electron.cjs

check_sh 'GLX reports a Mesa software renderer' 'timeout 15 glxinfo -B | grep -Ei "OpenGL renderer string:.*(llvmpipe|softpipe)"'
check 'Build OpenGL triangle fixture' cc gl.c -lEGL -lGL -o "$scratch/gl"
check 'OpenGL triangle on the desktop reads back foreground and background pixels' timeout 15 "$scratch/gl"
check 'Surfaceless EGL triangle reads back pixels without X' timeout 15 env -u DISPLAY EGL_PLATFORM=surfaceless "$scratch/gl"
check_sh 'Vulkan enumerates Mesa lavapipe' 'timeout 30 vulkaninfo --summary 2>/dev/null | grep -i "deviceName.*llvmpipe"'
check_sh 'Lavapipe executes a Vulkan compute shader and returns four expected values' '
    glslangValidator -V compute.comp -o "$scratch/compute.spv" &&
    cc vulkan.c -lvulkan -o "$scratch/vulkan" &&
    timeout 25 env VK_LOADER_DRIVERS_SELECT="*lvp*" "$scratch/vulkan" "$scratch/compute.spv"'
check_sh 'Cairo PNG and ImageMagick transform preserve expected colors' '
    cc cairo.c $(pkg-config --cflags --libs cairo) -o "$scratch/cairo" &&
    "$scratch/cairo" "$scratch/cairo.png" &&
    convert "$scratch/cairo.png" -resize 32x32 "$scratch/cairo-small.png" &&
    test "$(convert "$scratch/cairo-small.png" -format "%[pixel:p{16,16}] %[pixel:p{0,0}]" info:)" = "srgb(255,0,0) srgb(0,0,255)"'
check_sh 'Noto fallback and Pango render CJK, emoji and shaped Arabic' '
    fc-match -f "%{family}" "Noto Sans CJK JP:lang=ja" | grep -q "Noto Sans CJK" &&
    fc-match -f "%{family}" "emoji:charset=1f600" | grep -q "Noto Color Emoji" &&
    pango-view --no-display --font="Noto Sans 20" --text="日本語 😀 العربية" --output="$scratch/fonts.png" &&
    test "$(identify -format "%k" "$scratch/fonts.png")" -gt 20'
check_sh 'FFmpeg encodes and decodes 24 VP8 frames' '
    ffmpeg -v error -f lavfi -i testsrc2=size=160x120:rate=24 -frames:v 24 -c:v libvpx "$scratch/video.webm" &&
    test "$(ffprobe -v error -select_streams v:0 -count_frames -show_entries stream=nb_read_frames -of csv=p=0 "$scratch/video.webm")" = 24'
check 'PulseAudio null-sink monitor records a non-silent tone' timeout 20 ./audio.sh

for engine in chromium firefox webkit; do
    for mode in headless headed; do
        check "Playwright $engine $mode renders and responds to clicks" timeout 70 \
            "$mise" exec -- node browser.cjs "$playwright" "$engine" "$mode" "$scratch/video.webm"
    done
done
finish
