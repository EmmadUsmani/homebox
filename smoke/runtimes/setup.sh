#!/usr/bin/env bash
set -euo pipefail

if [[ ${SMOKE_LEVEL:-full} == quick ]]; then
    echo "Using the Node and Python runtimes provisioned by the guest profile"
else
    timeout --kill-after=10s 5m sudo -n apt-get update
    timeout --kill-after=10s 10m sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y \
        erlang-dev elixir php-cli php-mbstring php-xml php-zip composer \
        libyaml-dev libssl-dev libreadline-dev zlib1g-dev libsqlite3-dev

    timeout --kill-after=10s 30m mise use --global \
        bun@1 deno@2 ruby@3.4 java@temurin-21 maven@3 gradle@8 dotnet@10
fi
