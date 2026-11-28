#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo 'Usage: images/homebox-smoke/build.sh [--name IMAGE] [--export FILE] [--keep-vm]'
}

die() {
    printf '%s\n' "$*" >&2
    exit 1
}

image=homebox-smoke
archive=
keep_vm=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --name|--export)
            [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || die "Missing value for $1"
            if [[ "$1" == --name ]]; then image=$2; else archive=$2; fi
            shift 2
            ;;
        --keep-vm) keep_vm=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; die "Unknown argument: $1" ;;
    esac
done

[[ "$image" =~ ^[a-z][a-z0-9-]{0,30}$ ]] || die 'Invalid image name (use 1–31 lowercase letters, digits or hyphens, starting with a letter)'
host=$(uname -s)
case "$host/$(uname -m)" in
    Darwin/arm64|Linux/x86_64) ;;
    *) die 'This image requires Apple silicon macOS or x86-64 Linux.' ;;
esac
if [[ -n "$archive" ]]; then
    [[ "$archive" = /* ]] || archive="$PWD/$archive"
    [[ ! -e "$archive" && ! -L "$archive" ]] || die "Export path already exists: $archive"
    [[ -d $(dirname "$archive") && -w $(dirname "$archive") ]] || die "Export directory must exist and be writable: $(dirname "$archive")"
fi
if [[ -n "${SMOKE_NEWSDIGEST_REPO:-}" ]]; then
    git -C "$SMOKE_NEWSDIGEST_REPO" rev-parse --verify HEAD >/dev/null ||
        die 'SMOKE_NEWSDIGEST_REPO must point to a Git checkout with a committed HEAD.'
fi

repo=$(cd "$(dirname "$0")/../.." && pwd)
umask 077
export HOMEBOX_HOME
HOMEBOX_HOME=$(mktemp -d "$HOME/.hb-img.XXXXXX")
vm=smoke-build
box=${BOX:-$HOMEBOX_HOME/box}
phase='build box'
finished=false
index_lock=

cleanup() {
    local status=$?
    trap - EXIT
    if [[ -f "$HOMEBOX_HOME/boxes/$vm/box.json" ]]; then
        if $keep_vm; then
            if [[ $status -eq 0 ]] || "$box" stop "$vm"; then
                printf 'Build VM retained (stopped): %s\n' "$vm"
            else
                printf 'Build VM retained but could not be stopped: %s\n' "$vm" >&2
            fi
        elif ! "$box" rm "$vm" --force; then
            printf 'Cleanup failed for %s in %s\n' "$vm" "$HOMEBOX_HOME" >&2
            status=1
        fi
    fi
    if [[ -n "$index_lock" ]]; then rmdir "$index_lock" || status=1; fi
    if [[ $status -ne 0 ]]; then
        printf 'Build failed during %s. State: %s\n' "$phase" "$HOMEBOX_HOME" >&2
    elif $finished; then
        printf 'Local state: %s\n' "$HOMEBOX_HOME"
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ -z "${BOX:-}" ]]; then
    (cd "$repo" && go build -o "$box" ./cmd/box)
else
    box=$(command -v "$BOX") || die "BOX is not executable: $BOX"
    [[ "$box" = /* ]] || box="$PWD/$box"
    [[ -x "$box" ]] || die "BOX is not executable: $box"
fi
export BOX="$box"

phase='select a free port index'
# Match the per-index port assignments in internal/state/ports.go.
# Check both TCP and UDP listeners and reserve against other recipe runs.
if [[ "$host" == Linux ]]; then
    ports=$(ss -H -lntu | awk '{sub(/^.*:/, "", $5); print $5}' | sort -u)
else
    ports=$( (lsof -nP -i -Fn || [[ $? -eq 1 ]]) | sed -nE 's/^n[^>]*:([0-9]+)$/\1/p' | sort -u)
fi
for ((index = 99; index >= 1; index--)); do
    if awk -v i="$index" '
        $1 == 2200+i || $1 == 32200+i || $1 == 23000+i || $1 == 23100+i ||
        ($1 >= 10000+100*i && $1 < 10100+100*i) { busy = 1 }
        END { exit !busy }
    ' <<<"$ports"; then
        continue
    fi
    candidate="$HOME/.hb-img-port-$index"
    if mkdir "$candidate" 2>/dev/null; then
        index_lock=$candidate
        break
    fi
done
[[ -n "$index_lock" ]] || die 'No free Homebox port index.'
export HOMEBOX_FIRST_INDEX="$index"
printf 'Building %s in %s (index %s)\n' "$image" "$HOMEBOX_HOME" "$index"

phase='provision the build VM'
"$box" host init
"$box" new "$vm" --cpus 4 --memory 8 --disk 80 --force-provision
phase='install the desktop'
"$box" desktop "$vm" --install
phase='run the smoke suite'
SMOKE_LEVEL=full SMOKE_MATCH='' "$repo/smoke/run.sh" "$vm"
phase='check isolation'
"$box" doctor "$vm"
phase='check agent sign-in and remove dependency test fixtures'
cat "$repo/images/lib/prepare-guest.sh" "$repo/images/homebox-smoke/prepare-guest.sh" |
    "$box" ssh "$vm" -- 'bash -l -s'
phase='scan the guest for credentials'
"$box" ssh "$vm" -- 'sudo -n bash -s' < "$repo/images/lib/scan-secrets.sh"
phase='trim and stop the build VM'
"$box" ssh "$vm" -- 'sudo -n fstrim -av'
"$box" stop "$vm"
phase='save the image'
"$box" image save "$vm" "$image"
if [[ -n "$archive" ]]; then
    phase='export the image'
    "$box" image export "$image" "$archive"
    if [[ "$host" == Linux ]]; then
        archive_size=$(stat -c %s "$archive")
        archive_digest=$(sha256sum < "$archive")
    else
        archive_size=$(stat -f %z "$archive")
        archive_digest=$(shasum -a 256 < "$archive")
    fi
    printf 'Export: %s (%s bytes)\nArchive SHA-256: %s\n' "$archive" "$archive_size" "${archive_digest%% *}"
fi
phase='read saved image metadata'
"$box" image ls
printf 'Build duration: %ds\n' "$SECONDS"
finished=true
