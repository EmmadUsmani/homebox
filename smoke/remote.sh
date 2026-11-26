#!/bin/sh
set -eu

usage() {
    echo 'usage: smoke/remote.sh [-F SSH_CONFIG] ALIAS [AREA...]' >&2
    exit 2
}

config=${SMOKE_SSH_CONFIG:-}
while getopts F: option; do
    case "$option" in
        F) config=$OPTARG ;;
        *) usage ;;
    esac
done
shift $((OPTIND - 1))
[ "$#" -ge 1 ] || usage
alias=$1
shift
case "$alias" in ''|-*|.*|*[!a-zA-Z0-9_.-]*) usage ;; esac

here=$(cd "$(dirname "$0")" && pwd)
if [ "$#" -eq 0 ]; then
    for test in "$here"/*/test.sh; do
        set -- "$@" "$(basename "$(dirname "$test")")"
    done
fi
for area do
    case "$area" in ''|.*|*[!a-zA-Z0-9_-]*) usage ;; esac
    [ -f "$here/$area/test.sh" ] || { echo "No smoke area $area" >&2; exit 2; }
done

remote() {
    if [ -n "$config" ]; then
        set -- -F "$config" "$@"
    fi
    ssh -o BatchMode=yes -o ConnectTimeout=5 -o ServerAliveInterval=15 \
        -o ServerAliveCountMax=2 -o StrictHostKeyChecking=yes \
        -o ForwardAgent=no -o ForwardX11=no -o ClearAllForwardings=yes \
        -o ControlPath=none "$@"
}

ssh_settings=$(remote -G "$alias" 2>/dev/null)
SMOKE_LAN_HOST=$(printf '%s\n' "$ssh_settings" | awk '$1 == "hostname" {print $2; exit}')
SMOKE_ARTIFACTS=${SMOKE_RESULTS:-$here/../.smoke-results}/$alias-remote-$(date +%Y%m%d-%H%M%S)-$$
SMOKE_VM=$alias
SMOKE_SSH_CONFIG=$config
export SMOKE_LAN_HOST SMOKE_ARTIFACTS SMOKE_VM SMOKE_SSH_CONFIG
mkdir -p "$SMOKE_ARTIFACTS"
printf 'Logs: %s\n' "$SMOKE_ARTIFACTS"
archive=$(mktemp)
trap 'unlink "$archive"' EXIT

COPYFILE_DISABLE=1 tar --no-xattrs -C "$here" -czf "$archive" .
remote "$alias" 'rm -rf ~/homebox-smoke && mkdir ~/homebox-smoke && tar -xzf - -C ~/homebox-smoke' <"$archive"

failed=
for area do
    if [ -f "$here/$area/prepare.sh" ]; then
        printf 'SKIP %s needs its host prepare phase\n' "$area" |
            tee "$SMOKE_ARTIFACTS/$area-prepare.log"
        continue
    fi
    for phase in setup test remote; do
        [ -f "$here/$area/$phase.sh" ] || continue
        log="$SMOKE_ARTIFACTS/$area-$phase.log"
        printf '== %s %s\n' "$area" "$phase"
        status=0
        if [ "$phase" = remote ]; then
            sh "$here/$area/remote.sh" </dev/null >"$log" 2>&1 || status=$?
        else
            remote "$alias" "cd ~/homebox-smoke/$area && bash -l ./$phase.sh" </dev/null >"$log" 2>&1 || status=$?
        fi
        if [ "$status" -ne 0 ]; then
            if ! grep -q '^FAIL ' "$log"; then
                printf 'FAIL %s %s exited %s\n' "$area" "$phase" "$status" >>"$log"
            fi
            failed="$failed $area-$phase"
        fi
        cat "$log"
        [ "$phase" != setup ] || [ "$status" -eq 0 ] || break
    done
    if [ -f "$here/$area/host.sh" ]; then
        printf 'SKIP %s host phase requires box on the VM host\n' "$area" |
            tee "$SMOKE_ARTIFACTS/$area-host.log"
    fi
done

awk '/^PASS / {p++} /^FAIL / {f++} /^SKIP / {s++}
    END {printf "PASS %d\nFAIL %d\nSKIP %d\n", p, f, s}' "$SMOKE_ARTIFACTS"/*.log
if [ -n "$failed" ]; then
    printf 'Failed:%s\n' "$failed"
    exit 1
fi
