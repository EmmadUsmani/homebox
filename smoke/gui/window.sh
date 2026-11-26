#!/bin/sh
set -eu
gui_artifacts=${gui_artifacts:?set by test.sh}
title=$1
shift
"$@" &
app=$!
trap 'kill "$app" 2>/dev/null || true; wait "$app" 2>/dev/null || true' EXIT
while :; do
    if ! kill -0 "$app" 2>/dev/null; then
        wait "$app" || exit $?
        echo "Application exited before its window painted" >&2
        exit 1
    fi
    window=$(xdotool search --onlyvisible --name "^$title$" | head -n 1)
    if [ -z "$window" ]; then
        sleep 0.2
        continue
    fi
    import -window "$window" "$gui_artifacts/$title.png"
    dimensions=$(identify -format '%w %h' "$gui_artifacts/$title.png")
    if [ "$dimensions" != '320 160' ]; then
        printf 'Expected a 320 × 160 window, captured %s\n' "$dimensions" >&2
        exit 1
    fi
    # The fixtures paint the same solid green so a mapped but unpainted window fails.
    pixel=$(convert "$gui_artifacts/$title.png" -format '%[fx:p{100,100}.g] %[fx:p{100,100}.r]' info:)
    if awk -v pixel="$pixel" 'BEGIN { split(pixel, channels); exit !(channels[1] > 0.7 && channels[2] < 0.2) }'; then
        exit 0
    fi
    sleep 0.2
done
