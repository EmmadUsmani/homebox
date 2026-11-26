#!/bin/sh
set -u
# shellcheck source=smoke/lib.sh
. "$(dirname "$0")/../lib.sh"

box=${BOX:-box}
project="homebox-smoke-host-$$"
compose() {
    "$box" ssh "$SMOKE_VM" -- "cd ~/homebox-smoke/containers/fixtures/compose && timeout 240 docker compose -p $project $*"
}
cleanup() {
    compose down -v --remove-orphans --rmi local >/dev/null 2>&1
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

published_compose() {
    port=$("$box" ports "$SMOKE_VM" | awk '$1 == 8073 {print $2}') || return 1
    [ -n "$port" ] || { echo 'No box mapping for guest port 8073'; return 1; }
    compose up --build --wait --wait-timeout 120 || return 1
    response=$(curl --noproxy '*' --retry 10 --retry-connrefused --retry-max-time 25 \
        -fsS --max-time 5 "http://127.0.0.1:$port/") || return 1
    [ "$response" = compose:42:PONG ]
}
check 'Mac reaches the Compose web service through the box port mapping' published_compose
isolation="$(dirname "$0")/fixtures/host-isolation.cjs"
check "Docker bridge container cannot connect to a live Mac loopback canary" node "$isolation" loopback
status=0
node "$isolation" lan || status=$?
case "$status" in
    0) pass "Docker bridge container cannot connect to live Mac LAN-interface canaries" ;;
    77) skip "Docker LAN canaries require a non-loopback IPv4 interface" ;;
    *) fail "Docker bridge container cannot connect to live Mac LAN-interface canaries" ;;
esac
finish
