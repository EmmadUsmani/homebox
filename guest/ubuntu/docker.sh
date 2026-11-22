#!/bin/sh
set -eu

export DEBIAN_FRONTEND=noninteractive
admin=${HOMEBOX_USER:-${SUDO_USER:?admin user required}}
# shellcheck source=/dev/null
. /etc/os-release

key=$(mktemp)
trap 'rm -f "$key"' EXIT
curl -fsSL --proxy "${HOMEBOX_PROXY_URL:-}" --noproxy "${HOMEBOX_NO_PROXY:-}" \
    https://download.docker.com/linux/ubuntu/gpg -o "$key"
install -D -m 0644 "$key" /etc/apt/keyrings/docker.asc
cat > /etc/apt/sources.list.d/docker.sources <<APT
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: ${UBUNTU_CODENAME:-$VERSION_CODENAME}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
APT
apt-get update
apt-get install -y --no-install-recommends \
    docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
usermod -aG docker "$admin"
systemctl enable --now docker.service
