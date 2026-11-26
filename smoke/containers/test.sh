#!/usr/bin/env bash
# shellcheck disable=SC2016 # Quoted scripts expand in their target shells.
set -uo pipefail
# shellcheck source=smoke/lib.sh
. ../lib.sh

export fixtures="$PWD/fixtures"
export tools="$HOME/.cache/homebox-smoke/containers"
export PATH="$tools/bin:$PATH"
scratch=$(mktemp -d) || exit 1
export scratch
export prefix="homebox-smoke-$$"
export alpine=alpine:3.22
export nginx=nginx:1.28-alpine

cleanup() {
    timeout 45 docker compose -p "$prefix" -f "$fixtures/compose/compose.yaml" down -v --remove-orphans --rmi local >/dev/null 2>&1
    timeout 20 docker rm -f "$prefix-observe" "$prefix-memory" "$prefix-build-result" >/dev/null 2>&1
    timeout 20 docker volume rm "$prefix" >/dev/null 2>&1
    timeout 25 docker image rm "$prefix:amd64" "$prefix:cache-write" "$prefix:cache-read" >/dev/null 2>&1
    timeout 20 podman rmi "$prefix:podman" >/dev/null 2>&1
    timeout 45 env KUBECONFIG="$scratch/kubeconfig" kind delete cluster --name "$prefix" >/dev/null 2>&1
    timeout 35 bash -c '
        ids=$(docker ps -aq --filter "label=devcontainer.local_folder=$1")
        if [ -n "$ids" ]; then
            while IFS= read -r id; do docker rm -f "$id" || exit; done <<< "$ids"
        fi
    ' bash "$scratch/devcontainer" >/dev/null 2>&1
    rm -rf "$scratch"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

check_case() {
    export -f "${2:?missing case function}"
    check "$1" bash -eo pipefail -c '"$@"' bash "${@:2}"
}

distro() {
    local platform=$1 image=$2 script=$3
    timeout 360 docker run --rm --platform "$platform" "$image" sh -ec "$script"
}
architecture=$(dpkg --print-architecture) || exit 1
export native="linux/$architecture"
check 'Debian apt installs and runs jq' distro "$native" debian:bookworm-slim \
    'apt-get update && apt-get install -y --no-install-recommends jq && test "$(jq -n 42)" = 42'
check 'Ubuntu apt installs and runs jq' distro "$native" ubuntu:24.04 \
    'apt-get update && apt-get install -y --no-install-recommends jq && test "$(jq -n 42)" = 42'
check 'Alpine apk installs and runs jq' distro "$native" "$alpine" \
    'apk add --no-cache jq && test "$(jq -n 42)" = 42'
check 'Fedora dnf installs and runs jq' distro "$native" fedora:44 \
    'dnf -y install jq && test "$(jq -n 42)" = 42'
check 'Rocky Linux dnf installs and runs jq' distro "$native" rockylinux:9 \
    'dnf -y install jq && test "$(jq -n 42)" = 42'
check 'openSUSE zypper installs and runs jq' distro "$native" opensuse/tumbleweed:latest \
    'zypper --non-interactive install jq && test "$(jq -n 42)" = 42'
pacman_flags=
if [[ "$native" != linux/amd64 ]]; then
    pacman_flags=--disable-sandbox-syscalls
fi
check 'Arch Linux pacman installs and runs jq on amd64' distro linux/amd64 archlinux:base \
    "pacman -Sy --noconfirm $pacman_flags jq"' && test "$(jq -n 42)" = 42'
check 'BusyBox executes its awk applet' distro "$native" busybox:1.37 \
    'test "$(awk "BEGIN {print 6*7}")" = 42'
check 'Distroless debug image runs as nonroot' timeout 90 docker run --rm \
    --entrypoint /busybox/sh gcr.io/distroless/static-debian12:debug-nonroot \
    -ec 'test "$(/busybox/id -u)" = 65532 && test ! -e /bin/sh'
check 'Docker runs an amd64 executable' distro linux/amd64 "$alpine" 'test "$(uname -m)" = x86_64'

build_cache() (
    set -e
    for phase in write read; do
        timeout 240 docker buildx build --builder default --platform "$native" --load \
            --build-arg "CACHE_ID=${scratch##*/}" --build-arg "CACHE_PHASE=$phase" -t "$prefix:cache-$phase" "$fixtures/build"
    done
    timeout 20 docker create --name "$prefix-build-result" "$prefix:cache-read" /unused
    timeout 20 docker cp "$prefix-build-result:/result" "$scratch/native-result"
    grep -Fx "${native#linux/}:$(uname -m)" "$scratch/native-result"
)
check_case 'Buildx builds multiple stages and reuses a BuildKit cache mount' build_cache

build_amd64() (
    set -e
    timeout 240 docker buildx build --builder default --platform linux/amd64 \
        --load -t "$prefix:amd64" "$fixtures/build"
    test "$(timeout 15 docker image inspect --format '{{.Architecture}}' "$prefix:amd64")" = amd64
    timeout 20 docker rm -f "$prefix-build-result" >/dev/null 2>&1 || true
    timeout 20 docker create --platform linux/amd64 --name "$prefix-build-result" "$prefix:amd64" /unused
    timeout 20 docker cp "$prefix-build-result:/result" "$scratch/amd64-result"
    grep -Fx 'amd64:x86_64' "$scratch/amd64-result"
)
check_case 'Buildx executes an amd64 build stage and loads the resulting image' build_amd64

build_multi_platform() (
    set -e
    timeout 240 docker buildx build --builder default --platform linux/arm64,linux/amd64 \
        --output "type=local,dest=$scratch/platforms" "$fixtures/build"
    grep -Fx 'arm64:aarch64' "$scratch/platforms/linux_arm64/result"
    grep -Fx 'amd64:x86_64' "$scratch/platforms/linux_amd64/result"
)
check_case 'Buildx exports both arm64 and amd64 results from one multi-platform build' build_multi_platform

compose() {
    timeout 240 docker compose -p "$prefix" -f "$fixtures/compose/compose.yaml" "$@"
}
export -f compose

compose_stack() (
    set -e
    compose up --build --wait --wait-timeout 120
    test "$(curl -fsS --max-time 5 http://127.0.0.1:8073/)" = compose:42:PONG
)
compose_persistence() (
    set -e
    compose exec -T postgres psql -U postgres -d smoke -v ON_ERROR_STOP=1 \
        -c 'CREATE TABLE smoke_values (value int); INSERT INTO smoke_values VALUES (42);'
    test "$(compose exec -T redis redis-cli --raw SET smoke 42)" = OK
    test "$(compose exec -T redis redis-cli --raw SAVE)" = OK
    compose down
    compose up --wait --wait-timeout 120
    test "$(compose exec -T postgres psql -U postgres -d smoke -Atc 'SELECT value FROM smoke_values')" = 42
    test "$(compose exec -T redis redis-cli --raw GET smoke)" = 42
)
check_case 'Compose web connects to healthy Postgres and Redis by service DNS' compose_stack
check_case 'Compose named volumes persist database values across down and up' compose_persistence

mount_ownership() (
    set -e
    mkdir "$scratch/bind"
    timeout 20 docker volume create "$prefix"
    timeout 30 docker run --rm -v "$prefix:/volume" "$alpine" \
        chown "$(id -u):$(id -g)" /volume
    timeout 30 docker run --rm --user "$(id -u):$(id -g)" \
        -v "$prefix:/volume" -v "$scratch/bind:/bind" "$alpine" \
        sh -ec 'printf volume > /volume/file; printf bind > /bind/file'
    test "$(stat -c '%u:%g' "$scratch/bind/file")" = "$(id -u):$(id -g)"
    test "$(cat "$scratch/bind/file")" = bind
    timeout 30 docker run --rm -v "$prefix:/volume:ro" "$alpine" \
        sh -ec 'test "$(cat /volume/file)" = volume && ! touch /volume/readonly'
)
check_case 'Named and bind mounts preserve numeric ownership and read-only mounts' mount_ownership

observe() (
    set -e
    timeout 45 docker run -d --name "$prefix-observe" --memory 64m --cpus 0.5 \
        "$alpine" sh -c 'echo container-ready; sleep 600'
    test "$(timeout 20 docker inspect --format '{{.State.Running}}' "$prefix-observe")" = true
    test "$(timeout 20 docker exec "$prefix-observe" cat /sys/fs/cgroup/memory.max)" = 67108864
    test "$(timeout 20 docker exec "$prefix-observe" cat /sys/fs/cgroup/cpu.max)" = '50000 100000'
    timeout 20 docker stats --no-stream --format '{{.Name}}' "$prefix-observe" | grep -Fx "$prefix-observe"
    test "$(timeout 20 docker logs "$prefix-observe")" = container-ready
)
check_case 'Docker logs, inspect, exec and stats report a live CPU- and memory-limited container' observe
# Docker can report the exit before its OOMKilled flag is updated.
check 'Docker kills a process that exceeds its enforced memory limit' \
    timeout 120 docker run --rm --name "$prefix-memory" --memory 32m --memory-swap 32m \
    python:3.13-alpine sh -ec '
        exit_code=0
        python -c "memory = bytearray(128 * 1024 * 1024)" || exit_code=$?
        test "$exit_code" -eq 137
        grep -E "^oom_kill [1-9][0-9]*$" /sys/fs/cgroup/memory.events
    '

check 'Docker bridge container reaches a public HTTPS endpoint' timeout 90 docker run --rm \
    "$alpine" wget -q --timeout=15 -O /dev/null https://example.com/
private_egress() {
    local output
    # Reserved test destinations exercise private-address classes without probing LAN devices.
    output=$(timeout 30 docker run --rm "$alpine" sh -ec '
        command -v nc >/dev/null
        for target in 10.255.255.254 172.31.255.254 192.168.255.254 100.127.255.254 169.254.255.254; do
            if nc -w 2 "$target" 80 </dev/null >/dev/null 2>&1; then exit 1; fi
        done
        echo blocked
    ') && [[ "$output" = blocked ]]
}
check 'Docker bridge container cannot connect to private destinations' private_egress

podman_rootless() (
    set -e
    test "$(timeout 30 podman info --format '{{.Host.Security.Rootless}}')" = true
    timeout 90 docker save --platform "$native" "$alpine" | timeout 90 podman load
    timeout 30 podman tag "localhost/$alpine" "docker.io/library/$alpine"
    timeout 120 podman run --pull=never --rm --user 65532 "docker.io/library/$alpine" \
        sh -ec 'test "$(id -u)" = 65532'
    mkdir "$scratch/podman"
    printf 'FROM docker.io/library/%s\nRUN printf podman > /result\n' "$alpine" > "$scratch/podman/Containerfile"
    timeout 120 podman build --pull=never -t "$prefix:podman" "$scratch/podman"
    test "$(timeout 30 podman run --rm "$prefix:podman" cat /result)" = podman
)
check_case 'Rootless Podman runs a container and builds and runs its own image' podman_rootless

kubernetes() (
    set -e
    export KUBECONFIG="$scratch/kubeconfig"
    timeout 360 kind create cluster --name "$prefix" --wait 120s
    timeout 120 docker image save --platform "$native" --output "$scratch/nginx.tar" "$nginx"
    timeout 120 kind load image-archive "$scratch/nginx.tar" --name "$prefix"
    timeout 30 kubectl create deployment smoke --image="$nginx"
    timeout 130 kubectl rollout status deployment/smoke --timeout=120s
    timeout 45 kubectl port-forward deployment/smoke :80 > "$scratch/port-forward.log" 2>&1 &
    local forward=$!
    trap 'kill "$forward" 2>/dev/null || true; wait "$forward" 2>/dev/null || true' EXIT
    local port
    for _ in {1..20}; do
        port=$(awk '/Forwarding from 127/ {split($3, address, ":"); print address[2]}' "$scratch/port-forward.log")
        if [[ -n "$port" ]]; then
            curl -fsS --max-time 5 "http://127.0.0.1:$port/" | grep -F 'Welcome to nginx'
            return
        fi
        sleep 1
    done
    cat "$scratch/port-forward.log"
    return 1
)
check_case 'kind deploys an nginx workload reachable through kubectl port-forward' kubernetes

devcontainer() (
    set -e
    cp -R "$fixtures/devcontainer" "$scratch/devcontainer"
    local workspace="$scratch/devcontainer"
    local cli="$tools/node_modules/.bin/devcontainer"
    timeout 180 "$cli" up --workspace-folder "$workspace"
    timeout 30 "$cli" exec --workspace-folder "$workspace" sh -ec \
        'test "$(id -un)" = vscode && test "$(cat /tmp/homebox-devcontainer)" = ready'
)
check_case 'Dev Containers builds, starts, runs postCreateCommand and executes as remoteUser' devcontainer

cp "$fixtures/testcontainers/smoke.test.cjs" "$tools/" || exit 1
check 'Node Testcontainers starts a service, waits for HTTP and exercises its mapped port' \
    timeout 180 node --test "$tools/smoke.test.cjs"

# shellcheck source=smoke/containers/minimal.sh
. ./minimal.sh

finish
