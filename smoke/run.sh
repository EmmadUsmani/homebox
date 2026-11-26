#!/bin/sh
# Run guest smoke tests against a VM from the Mac hosting it.
#
#   smoke/run.sh NAME [AREA...]
#
# Copies smoke/ into the guest (~/homebox-smoke), then for each area runs
# prepare.sh on this Mac, setup.sh and test.sh inside the guest as the VM user
# (bash login shell), and host.sh on this Mac, where present. With no AREA it
# runs every area. Logs go to .smoke-results/NAME-TIMESTAMP/. Set BOX to use a
# box binary other than the one on PATH, and HOMEBOX_HOME for another state dir.
set -eu
[ $# -ge 1 ] || { echo 'usage: smoke/run.sh NAME [AREA...]' >&2; exit 2; }
box=${BOX:-box}
name=$1
shift
here=$(cd "$(dirname "$0")" && pwd)
if [ $# -eq 0 ]; then
    # shellcheck disable=SC2046 # area names are directory names without spaces
    set -- $(cd "$here" && for d in */; do [ -f "$d/test.sh" ] && printf '%s ' "${d%/}"; done)
fi
results=${SMOKE_RESULTS:-$here/../.smoke-results}/$name-$(date +%Y%m%d-%H%M%S)
mkdir -p "$results"

# Keep macOS resource forks and extended attributes out of guest build contexts.
COPYFILE_DISABLE=1 tar --no-xattrs -C "$here" -czf - . | "$box" ssh "$name" -- \
    'rm -rf ~/homebox-smoke && mkdir -p ~/homebox-smoke && tar -xzf - -C ~/homebox-smoke'

failed=
for area in "$@"; do
    [ -d "$here/$area" ] || { echo "No smoke area $area" >&2; failed="$failed $area"; continue; }
    if [ -f "$here/$area/prepare.sh" ]; then
        log="$results/$area-prepare.log"
        printf '== %s prepare\n' "$area"
        status=0
        SMOKE_VM=$name BOX=$box sh "$here/$area/prepare.sh" </dev/null >"$log" 2>&1 || status=$?
        cat "$log"
        if [ "$status" -ne 0 ]; then
            [ "$status" -eq 77 ] || failed="$failed $area-prepare"
            continue
        fi
    fi
    if [ -f "$here/$area/images.txt" ] || [ "$area" = newsdigest ]; then
        log="$results/$area-images.log"
        printf '== %s images\n' "$area"
        status=0
        BOX=$box "$here/cache.sh" "$name" "$area" </dev/null >"$log" 2>&1 || status=$?
        cat "$log"
        if [ "$status" -ne 0 ]; then
            failed="$failed $area-images"
            continue
        fi
    fi
    for phase in setup test; do
        [ -f "$here/$area/$phase.sh" ] || continue
        log="$results/$area-$phase.log"
        printf '== %s %s\n' "$area" "$phase"
        status=0
        "$box" ssh "$name" -- "cd ~/homebox-smoke/$area && bash -l ./$phase.sh" </dev/null >"$log" 2>&1 || status=$?
        cat "$log"
        if [ "$status" -ne 0 ]; then
            failed="$failed $area-$phase"
            [ "$phase" = setup ] && break
        fi
    done
    if [ -f "$here/$area/host.sh" ]; then
        log="$results/$area-host.log"
        printf '== %s host\n' "$area"
        status=0
        SMOKE_VM=$name BOX=$box SMOKE_ARTIFACTS=$results sh "$here/$area/host.sh" </dev/null >"$log" 2>&1 || status=$?
        cat "$log"
        [ "$status" -eq 0 ] || failed="$failed $area-host"
    fi
done

printf '\nLogs: %s\n' "$results"
printf 'PASS %s\nFAIL %s\nSKIP %s\n' \
    "$(cat "$results"/*.log | grep -c '^PASS' || true)" \
    "$(cat "$results"/*.log | grep -c '^FAIL' || true)" \
    "$(cat "$results"/*.log | grep -c '^SKIP' || true)"
if [ -n "$failed" ]; then
    printf 'Failed:%s\n' "$failed"
    exit 1
fi
