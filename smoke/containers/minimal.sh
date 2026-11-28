# Sourced by test.sh after the Docker platform checks.
# shellcheck shell=bash
# shellcheck disable=SC2154,SC2016 # test.sh provides exports and the sidecar expands its script.

minimal_build() {
    local platform=$1 image=$2 dockerfile=$3
    shift 3
    timeout 600 docker build --platform "$platform" -t "$image" \
        -f "$fixtures/minimal/Dockerfile.$dockerfile" "$@" "$fixtures/minimal"
    test "$(timeout 15 docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")" = "$platform"
}

minimal_no_shell() {
    # Inspect the merged filesystem, including symlinks, without executing a shell in it.
    timeout 90 docker export "$1" | tar -tf - | awk '
        /(^|\/)s?bin\/(sh|ash|bash|dash|busybox|ksh|mksh|zsh|fish|csh|tcsh|yash)$/ ||
        /^busybox\/(sh|ash|bash|busybox)$/ { print "shell in runtime: " $0; found=1 }
        END { exit found }
    '
}

minimal_cleanup() {
    timeout 15 docker logs "$container" || true
    timeout 20 docker rm -f "$container-debug" "$container" >/dev/null 2>&1 || true
    timeout 20 docker image rm "$image" >/dev/null 2>&1 || true
}

minimal_service() {
    local platform=$1 name=$2 expected=$3 dockerfile=$4
    shift 4
    local port response=
    container="$prefix-$name" image="$prefix:$name"
    trap minimal_cleanup EXIT
    minimal_build "$platform" "$image" "$dockerfile" "$@"
    timeout 40 docker run -d --platform "$platform" --name "$container" -p 127.0.0.1::8080 "$image"
    port=$(timeout 15 docker port "$container" 8080/tcp)
    for _ in {1..45}; do
        if response=$(curl -fsS --max-time 2 "http://$port/") && [[ "$response" == "$expected" ]]; then
            break
        fi
        test "$(timeout 15 docker inspect --format '{{.State.Running}}' "$container")" = true
        sleep 1
    done
    test "$response" = "$expected"
    # Docker needs the PID column to filter host ps output to the container.
    timeout 15 docker top "$container" -o pid,uid | awk '
        NR > 1 { if ($2 !~ /^[0-9]+$/ || $2 == 0) bad=1 }
        END { exit (bad || NR < 2) }
    '
    minimal_no_shell "$container"
    if [[ "$name" == node && "$platform" == "$native" ]]; then
        timeout 60 docker run --rm --name "$container-debug" \
            --pid="container:$container" --network="container:$container" busybox:1.37 sh -ec '
                tr "\000" "\n" </proc/1/cmdline | grep -Fx /app/server.cjs
                test "$(wget -q -T 5 -O - http://127.0.0.1:8080/)" = "$1"
            ' sh "$expected"
    fi
}

minimal_program() {
    local platform=$1 name=$2 expected=$3 dockerfile=$4
    shift 4
    local output
    container="$prefix-$name" image="$prefix:$name"
    trap minimal_cleanup EXIT
    minimal_build "$platform" "$image" "$dockerfile" "$@"
    output=$(timeout 60 docker run --platform "$platform" --name "$container" "$image")
    printf '%s\n' "$output"
    test "$output" = "$expected"
    minimal_no_shell "$container"
}

export -f minimal_build minimal_no_shell minimal_cleanup
platforms=("$native")
if [[ ${SMOKE_LEVEL:-full} == full ]]; then
    platforms+=("$foreign")
fi
for platform in "${platforms[@]}"; do
    node_label="Distroless Node serves HTTP using node_modules"
    if [[ "$platform" == "$native" ]]; then
        node_label+=" and a BusyBox sidecar sees PID 1 and reaches HTTP"
    fi
    check_case "$node_label ($platform)" \
        minimal_service "$platform" node 'node:&lt;42&gt;' node --target distroless
    full_only check_case "Distroless Python runs pure Python and a native extension ($platform)" \
        minimal_program "$platform" python $'python:pure:42\npython:native:42' python --target distroless
    full_only check_case "Distroless Java serves HTTP from a Maven-built jar ($platform)" \
        minimal_service "$platform" java 'java:42' java
    full_only check_case "Distroless cc runs C++ with glibc and libstdc++ ($platform)" \
        minimal_program "$platform" cpp 'cpp:42' cpp
    full_only check_case "Distroless base runs a dynamically linked cgo binary ($platform)" \
        minimal_program "$platform" cgo 'cgo:42' golang --target base
    full_only check_case "Distroless static Go validates HTTPS with its CA bundle ($platform)" \
        minimal_program "$platform" static 'https:200' golang --target static
    full_only check_case "Scratch static Go validates HTTPS with a copied CA bundle ($platform)" \
        minimal_program "$platform" scratch 'https:200' golang --target bare
    full_only check_case "Chainguard Node slim serves HTTP using node_modules ($platform)" \
        minimal_service "$platform" wolfi-node 'node:&lt;42&gt;' node \
        --target wolfi
    full_only check_case "Chainguard Python runs a pure-Python dependency ($platform)" \
        minimal_program "$platform" wolfi-python 'python:pure:42' python --target wolfi
    full_only check_case "Ubuntu chiseled ASP.NET serves HTTP ($platform)" \
        minimal_service "$platform" dotnet 'dotnet:42' dotnet
done
