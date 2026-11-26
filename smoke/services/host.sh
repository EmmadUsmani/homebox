#!/bin/sh
# shellcheck disable=SC2016 # Commands expand on the guest or in the child shell.

set -eu
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

remote() { "$BOX" ssh "$SMOKE_VM" -- "$@"; }
cleanup() {
    remote 'systemctl --user stop homebox-smoke-http homebox-smoke-ws' >/dev/null 2>&1 || true
}
trap cleanup EXIT

remote '
    set -eu
    cd ~/homebox-smoke/services
    systemd-run --user --unit=homebox-smoke-http --collect "$PWD/.venv/bin/python" "$PWD/web.py" http 0.0.0.0 8000
    systemd-run --user --unit=homebox-smoke-ws --collect "$PWD/.venv/bin/python" "$PWD/web.py" ws 0.0.0.0 8001
'
mappings=$("$BOX" ports "$SMOKE_VM")

mapping() {
    printf '%s\n' "$mappings" | awk -v port="$1" '$1 == port {print $2, $3}'
}
http_url=$("$BOX" open "$SMOKE_VM" 8000 --print)
ws_url=$("$BOX" open "$SMOKE_VM" 8001 --print)
http_port=$(printf '%s\n' "$(mapping 8000)" | cut -d ' ' -f 1)
ws_port=$(printf '%s\n' "$(mapping 8001)" | cut -d ' ' -f 1)

listeners() {
    [ "$(mapping 8000)" = "$http_port 127.0.0.1" ] &&
        [ "$(mapping 8001)" = "$ws_port 127.0.0.1" ] &&
        [ "$http_url" = "http://127.0.0.1:$http_port/" ] &&
        [ "$ws_url" = "http://127.0.0.1:$ws_port/" ] &&
        [ "$(lsof -nP -a -iTCP:"$http_port" -sTCP:LISTEN -Fn | sed -n 's/^n//p')" = "127.0.0.1:$http_port" ] &&
        [ "$(lsof -nP -a -iTCP:"$ws_port" -sTCP:LISTEN -Fn | sed -n 's/^n//p')" = "127.0.0.1:$ws_port" ]
}
http() {
    content=$(curl --noproxy '*' --retry 5 --retry-all-errors --retry-max-time 12 --max-time 3 -fsS "$http_url") &&
        [ "$content" = homebox-smoke ]
}
websocket() {
    node - "ws:${ws_url#http:}" <<'JS'
const client = new WebSocket(process.argv[2]);
const timer = setTimeout(() => process.exit(1), 5000);
client.addEventListener('open', () => client.send('homebox smoke'));
client.addEventListener('error', () => process.exit(1));
client.addEventListener('message', event => {
  clearTimeout(timer);
  process.exit(event.data === 'homebox smoke' ? 0 : 1);
});
JS
}

check 'Mac reaches guest HTTP through a published port' http
check 'Mac reaches guest WebSocket echo through a published port' websocket
check 'box ports and box open agree with actual Mac loopback listeners' listeners
check 'box doctor verifies host and private-network isolation' "$BOX" doctor "$SMOKE_VM"
finish
