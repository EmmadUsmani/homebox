#!/bin/sh
set -eu
scratch=${scratch:?set by test.sh}
export XDG_RUNTIME_DIR="$scratch/pulse"
mkdir -m 700 "$XDG_RUNTIME_DIR"
pulseaudio --start --exit-idle-time=-1
record=
trap 'if [ -n "$record" ]; then kill "$record" 2>/dev/null || true; fi; pulseaudio --kill' EXIT
pactl load-module module-null-sink sink_name=smoke >/dev/null
ffmpeg -v error -f lavfi -i sine=frequency=440:duration=2 "$scratch/tone.wav"
parec --device=smoke.monitor --rate=44100 --channels=1 --format=s16le --latency-msec=50 >"$scratch/recorded.pcm" &
record=$!
until pactl list short source-outputs | grep -q .; do
    kill -0 "$record"
    sleep 0.1
done
paplay --device=smoke "$scratch/tone.wav"
kill "$record"
wait "$record" || true
record=
/usr/bin/python3 - "$scratch/recorded.pcm" <<'PY'
import array
import sys

samples = array.array("h")
with open(sys.argv[1], "rb") as recording:
    samples.frombytes(recording.read())
assert sum(abs(sample) > 500 for sample in samples) > 1000, "loopback recording is silent"
PY
