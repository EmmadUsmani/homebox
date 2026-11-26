#!/bin/sh
set -eu
scratch=${scratch:?set by test.sh}
pulse="$scratch/pulse"
mkdir -m 700 "$pulse"
export PULSE_RUNTIME_PATH="$pulse" PULSE_STATE_PATH="$pulse"
export PULSE_SERVER="unix:$pulse/native" PULSE_COOKIE="$pulse/cookie"
pulseaudio -n --daemonize=no --use-pid-file=no --exit-idle-time=-1 \
    -L "module-native-protocol-unix socket=$pulse/native auth-cookie=$PULSE_COOKIE" \
    -L "module-null-sink sink_name=smoke" &
server=$!
record=
cleanup() {
    if [ -n "$record" ]; then
        kill "$record" 2>/dev/null || true
        wait "$record" 2>/dev/null || true
    fi
    kill "$server" 2>/dev/null || true
    wait "$server" 2>/dev/null || true
}
trap cleanup EXIT
until pactl info >/dev/null 2>&1; do
    kill -0 "$server"
    sleep 0.1
done
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
