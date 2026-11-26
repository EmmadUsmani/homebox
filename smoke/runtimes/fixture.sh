#!/usr/bin/env bash
# Sourced by scenario scripts running from the area directory.

set -euo pipefail
work=$(mktemp -d "$HOME/homebox-runtime.XXXXXXXX")
trap 'rm -rf "$work"' EXIT
cd "$work"

expect_http() (
    local path=$1 expected=$2
    shift 2
    "$@" >server.log 2>&1 &
    local server_pid=$!
    trap 'kill "$server_pid" 2>/dev/null || :; wait "$server_pid" 2>/dev/null || :' EXIT

    for ((attempt=0; attempt<60; attempt++)); do
        if curl -fsS --noproxy "*" --max-time 2 "http://127.0.0.1:18765$path" >response 2>/dev/null &&
            grep -Fq -- "$expected" response; then
            return 0
        fi
        kill -0 "$server_pid" 2>/dev/null || break
        sleep 1
    done
    cat server.log response >&2
    return 1
)
