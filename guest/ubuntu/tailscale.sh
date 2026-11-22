#!/bin/sh
set -eu

export DEBIAN_FRONTEND=noninteractive
# shellcheck source=/dev/null
. /etc/os-release

key=$(mktemp)
trap 'rm -f "$key"' EXIT
curl -fsSL --proxy "${HOMEBOX_PROXY_URL:-}" --noproxy "${HOMEBOX_NO_PROXY:-}" \
    "https://pkgs.tailscale.com/stable/ubuntu/$VERSION_CODENAME.noarmor.gpg" -o "$key"
install -D -m 0644 "$key" /etc/apt/keyrings/tailscale.gpg
printf 'deb [signed-by=/etc/apt/keyrings/tailscale.gpg] https://pkgs.tailscale.com/stable/ubuntu %s main\n' \
    "$VERSION_CODENAME" > /etc/apt/sources.list.d/tailscale.list
apt-get update
apt-get install -y --no-install-recommends tailscale
systemctl disable --now tailscaled.service
