#!/bin/sh
set -u
# shellcheck source=smoke/lib.sh
. "$(dirname "$0")/../lib.sh"

box=${BOX:-box}
project="homebox-smoke-host-$$"
compose() {
    "$box" ssh "$SMOKE_VM" -- "cd ~/homebox-smoke/containers/fixtures/compose && timeout 240 docker compose -p $project $*"
}
node_cleanup() {
    "$box" ssh "$SMOKE_VM" -- "timeout 20 docker rm -f $project-node; timeout 20 docker image rm $project:node" >/dev/null 2>&1
}
cleanup() {
    node_cleanup
    compose down -v --remove-orphans --rmi local >/dev/null 2>&1
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

published_http() {
    expected=$1
    shift
    port=$("$box" ports "$SMOKE_VM" | awk '$1 == 8073 {print $2}') || return 1
    [ -n "$port" ] || { echo 'No box mapping for guest port 8073'; return 1; }
    "$@" || return 1
    response=$(curl --noproxy '*' --retry 10 --retry-all-errors --retry-max-time 25 \
        -fsS --max-time 5 "http://127.0.0.1:$port/") || return 1
    [ "$response" = "$expected" ]
}

start_node() {
    "$box" ssh "$SMOKE_VM" -- "cd ~/homebox-smoke/containers/fixtures/minimal &&
        timeout 600 docker build --target distroless -t $project:node -f Dockerfile.node . &&
        timeout 40 docker run -d --name $project-node -p 127.0.0.1:8073:8080 $project:node"
}

check 'Mac reaches the Compose web service through the box port mapping' \
    published_http 'compose:42:PONG' compose up --build --wait --wait-timeout 120
compose down -v --remove-orphans --rmi local >/dev/null 2>&1
full_only check 'Mac reaches the distroless Node app through the box port mapping' \
    published_http 'node:&lt;42&gt;' start_node
node_cleanup
isolation="$(dirname "$0")/fixtures/host-isolation.cjs"
check "Docker bridge container cannot connect to a live Mac loopback canary" node "$isolation" loopback
if smoke_matches 'Docker bridge container cannot connect to live Mac LAN-interface canaries'; then
    status=0
    node "$isolation" lan || status=$?
    case "$status" in
        0) pass "Docker bridge container cannot connect to live Mac LAN-interface canaries" ;;
        77) skip "Docker LAN canaries require a non-loopback IPv4 interface" ;;
        *) fail "Docker bridge container cannot connect to live Mac LAN-interface canaries" ;;
    esac
fi
finish
