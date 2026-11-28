#!/bin/sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=smoke/lib.sh
. "$here/../lib.sh"

cleanup() {
    "$BOX" ssh "$SMOKE_VM" -- 'cd ~/homebox-smoke/newsdigest && bash -l ./test.sh --cleanup'
}

work=$(mktemp -d)
trap 'rm -rf "$work"; cleanup' EXIT

ports=$("$BOX" ports "$SMOKE_VM")
port=$(printf '%s\n' "$ports" | awk '$1 == 8000 {print $2}')
if [ -z "$port" ]; then
    fail 'News Digest guest port 8000 has no host forward'
    finish
fi
origin="http://127.0.0.1:$port"
artifacts=$(cd "$SMOKE_ARTIFACTS" && pwd)
check 'Host curl reaches published News Digest API health' \
    curl -fsS --max-time 10 "$origin/healthz"
check 'Host curl reaches published News Digest HTML' \
    curl -fsS --max-time 10 "$origin/"

collect() {
    "$BOX" ssh "$SMOKE_VM" -- 'cd ~/homebox-smoke/newsdigest && tar -cf - artifacts' >"$work/guest.tar" &&
        mkdir -p "$artifacts/newsdigest-guest" &&
        tar -xf "$work/guest.tar" -C "$artifacts/newsdigest-guest" --strip-components 1
}
check 'Copy guest browser screenshots and suite logs to the run output directory' collect

host_browser() {
    npm install --prefix "$work" --no-audit --no-fund --fetch-timeout=30000 --fetch-retries=1 playwright@1.64.1 &&
        PLAYWRIGHT_DOWNLOAD_CONNECTION_TIMEOUT=30000 "$work/node_modules/.bin/playwright" install chromium &&
        node "$here/browser.cjs" "$work/node_modules/playwright" "$origin" "$artifacts/newsdigest-host"
}
full_only check 'Host Chromium renders persisted edition through the published port' host_browser

check 'Stop the guest API and remove its disposable Compose data' cleanup
trap 'rm -rf "$work"' EXIT
finish
