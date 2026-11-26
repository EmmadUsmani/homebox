#!/bin/sh
set -eu
# shellcheck source=smoke/lib.sh
. "$(dirname "$0")/../lib.sh"

repo=${SMOKE_NEWSDIGEST_REPO:-}
if [ -z "$repo" ] || [ ! -d "$repo" ]; then
    skip 'News Digest checkout unavailable (set SMOKE_NEWSDIGEST_REPO)'
    exit 77
fi

archive=$(mktemp)
trap 'rm -f "$archive"' EXIT
revision=$(git -C "$repo" rev-parse --verify HEAD)
paths=$(git -C "$repo" ls-tree -r --name-only "$revision")
if printf '%s\n' "$paths" | grep -Eq '(^|/)(\.env[^/]*|node_modules)(/|$)'; then
    fail 'News Digest archive contains a forbidden path'
    finish
fi
git -C "$repo" archive --format=tar.gz "$revision" >"$archive"
transfer() {
    "$BOX" ssh "$SMOKE_VM" -- \
        'cd ~/homebox-smoke/newsdigest && mkdir source && tar -xzf - -C source' <"$archive"
}
label="Transfer News Digest source at $revision over pinned SSH"
if smoke_matches "$label"; then
    check "$label" transfer
else
    transfer || fail "$label"
fi
finish
