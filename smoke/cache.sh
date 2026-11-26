#!/bin/sh
# Populate a scratch guest's Docker store over pinned SSH.
set -eu
umask 077
cache=${SMOKE_IMAGE_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/homebox/smoke-images}/v1
if [ "$#" -eq 1 ] && [ "$1" = prune ]; then
    if [ -d "$cache" ]; then
        find "$cache" -type f -name '*.tar' -delete
    fi
    exit
fi

[ "$#" -eq 2 ] || { echo 'usage: smoke/cache.sh VM AREA | prune' >&2; exit 2; }
box=${BOX:-box}
name=$1
area=$2
here=$(cd "$(dirname "$0")" && pwd)

# shellcheck source=smoke/lib.sh
. "$here/lib.sh"

guest() { "$box" ssh "$name" -- "$1"; }

pull() {
    delay=5
    while ! output=$(guest "timeout 600 docker pull --platform '$2' '$1'" 2>&1); do
        printf '%s\n' "$output" >&2
        case "$output" in
            *429*|*toomanyrequests*) [ "$delay" -le 20 ] || return 1 ;;
            *) return 1 ;;
        esac
        printf 'Retrying %s in %s seconds\n' "$1" "$delay" >&2
        sleep "$delay"
        delay=$((delay * 2))
    done
}

image_archive() {
    key=$(printf '%s\n' "$1" "$2" | shasum -a 256) || return
    printf '%s/%s.tar\n' "$cache" "${key%% *}"
}

stage_image() (
    ref=$1
    platforms=$2
    tag=${ref%@*}
    case "$ref$tag" in
        *[!a-zA-Z0-9._/@:-]*) echo "Invalid smoke image reference: $ref" >&2; exit 1 ;;
    esac
    archive=$(image_archive "$ref" "$platforms") || exit 1
    if [ "${SMOKE_IMAGE_CACHE_REFRESH:-0}" != 1 ]; then
        if [ -f "$archive" ]; then
            printf 'Cache hit: %s (%s)\n' "$ref" "$platforms"
            guest "timeout 600 docker load" <"$archive"
            exit
        fi
        if [ "$3" != "$platforms" ]; then
            multi_archive=$(image_archive "$ref" "$3") || exit 1
            if [ -f "$multi_archive" ]; then
                printf 'Cache hit: %s (%s from multi-platform archive)\n' "$ref" "$platforms"
                guest "timeout 600 docker load --platform '$platforms'" <"$multi_archive"
                exit
            fi
        fi
    fi

    printf 'Cache miss: %s (%s)\n' "$ref" "$platforms"
    for platform in $(printf '%s' "$platforms" | tr ',' ' '); do
        pull "$ref" "$platform" || exit 1
    done
    if [ "$tag" != "$ref" ]; then
        guest "docker tag '$ref' '$tag'" || exit 1
        # Export the index so the loaded image retains its pinned digest.
        save="docker save '$tag'"
    else
        save="docker save --platform '$platforms' '$tag'"
    fi
    temporary=$(mktemp "$cache/.image.XXXXXXXX") || exit 1
    trap 'rm -f "$temporary"' EXIT
    guest "timeout 600 $save" >"$temporary" || exit 1
    mv -f "$temporary" "$archive"
)

case "$area" in
    newsdigest)
        images=$(guest 'cd ~/homebox-smoke/newsdigest/source && docker compose config --images')
        manifest=$(printf '%s\n' "$images" | sort -u)
        ;;
    *)
        manifest=$(cat "$here/$area/images.txt")
        ;;
esac
native="linux/$(guest 'dpkg --print-architecture')"
mkdir -p "$cache"
while read -r ref targets level; do
    case "$ref" in ''|\#*) continue ;; esac
    [ "$area" = newsdigest ] || smoke_image_enabled "$level" || continue
    case "$targets" in
        ''|native) platforms=$native ;;
        both) platforms=linux/amd64,linux/arm64 ;;
        amd64) platforms=linux/amd64 ;;
        *) echo "Invalid smoke platform selector: $targets" >&2; exit 1 ;;
    esac
    selected_platforms=$platforms
    [ "${SMOKE_LEVEL:-full}" = full ] || selected_platforms=$native
    stage_image "$ref" "$selected_platforms" "$platforms" </dev/null || exit 1
done <<EOF_MANIFEST
$manifest
EOF_MANIFEST
