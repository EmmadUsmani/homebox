#!/usr/bin/env bash
set -euo pipefail

sudo -n timeout 240 apt-get -o DPkg::Lock::Timeout=60 update
sudo -n timeout 600 env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=60 install -y --no-install-recommends \
    clang cmake ninja-build meson autoconf automake gdb valgrind strace ltrace linux-perf \
    zlib1g-dev libssl-dev libcurl4-openssl-dev libsqlite3-dev libuv1-dev libncurses-dev \
    musl-tools qemu-user qemu-user-binfmt nix-bin nix-setup-systemd

# Ubuntu restricts the packaged daemon socket to the nix-users group.
sudo -n usermod -aG nix-users "$(id -un)"

timeout 300 mise install go@1.25.14 zig@0.15.2

installer=$(mktemp)
trap 'rm -f "$installer"' EXIT
if [[ ! -x "$HOME/.cargo/bin/rustup" ]]; then
    curl --fail --silent --show-error --location --max-time 60 https://sh.rustup.rs -o "$installer"
    timeout 300 sh "$installer" -y --no-modify-path --profile minimal --default-toolchain none
fi
timeout 300 "$HOME/.cargo/bin/rustup" toolchain install 1.92.0 \
    --profile minimal --component rustfmt --component clippy --target wasm32-wasip1
