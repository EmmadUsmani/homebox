#!/usr/bin/env bash
set -euo pipefail

usage() {
    printf 'Usage: %s [--host-only]\n' "${0##*/}"
}

case "${1:-}" in
    --help|-h) usage; exit 0 ;;
    --host-only) dev=false; shift ;;
    '') dev=true ;;
    *) usage >&2; exit 2 ;;
esac
if (( $# != 0 )); then
    usage >&2
    exit 2
fi

if (( EUID == 0 )); then
    printf 'Run this script as your regular user. It calls sudo for system changes.\n' >&2
    exit 1
fi

# shellcheck source=/dev/null
. /etc/os-release
arch=$(dpkg --print-architecture)
if [[ $ID != ubuntu || $arch != amd64 ]]; then
    printf 'This setup requires Ubuntu on amd64.\n' >&2
    exit 1
fi

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
owner=$(id -un)
lima_version=2.3.0
lima_sha256=cc8ba97f1c801b899f7c9eda9919dfc4ce4fa2c140c53e5add15aa2d0a1936f9
host_packages=(ca-certificates curl qemu-system-x86 qemu-utils ovmf nftables openssh-client)
dev_packages=(build-essential pkg-config git gh jq ripgrep fd-find tmux unzip zip sqlite3 shellcheck)
desktop_packages=(libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev)
docker_packages=(docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin)

sudo -v
scratch=$(mktemp -d "$HOME/.homebox-setup.XXXXXXXX")
trap 'rm -rf -- "$scratch"' EXIT

printf 'Installing Ubuntu host packages\n'
sudo apt-get update
sudo apt-get install -y "${host_packages[@]}"
getent group kvm >/dev/null || sudo groupadd --system kvm
sudo usermod -aG kvm "$owner"

printf 'Installing Lima %s from its verified release archive\n' "$lima_version"
lima_archive="lima-$lima_version-Linux-x86_64.tar.gz"
curl -fsSL "https://github.com/lima-vm/lima/releases/download/v$lima_version/$lima_archive" -o "$scratch/$lima_archive"
(
    cd "$scratch"
    printf '%s  %s\n' "$lima_sha256" "$lima_archive" | sha256sum --check
)
sudo tar --extract --gzip --file "$scratch/$lima_archive" --directory /usr/local --no-same-owner
dpkg-query -W "${host_packages[@]}"
/usr/local/bin/limactl --version
qemu-system-x86_64 --version
qemu-img --version
nft --version
if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
    printf 'Check that /dev/kvm exists and sign in again to activate kvm group membership.\n'
fi

if [[ $dev == false ]]; then
    exit 0
fi

printf 'Installing contributor packages\n'
sudo apt-get install -y "${dev_packages[@]}" "${desktop_packages[@]}"
dpkg-query -W "${dev_packages[@]}" "${desktop_packages[@]}"

go_version=$(awk '$1 == "go" { print $2; exit }' "$repo/go.mod")
if [[ ! $go_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf 'Expected an exact Go release in go.mod, found %s\n' "$go_version" >&2
    exit 1
fi
go_root="$HOME/.local/opt/go$go_version"
mkdir -p "$HOME/.local/bin" "$HOME/.local/opt"
if [[ ! -x $go_root/bin/go ]]; then
    printf 'Installing Go %s from its verified release archive\n' "$go_version"
    go_archive="go$go_version.linux-$arch.tar.gz"
    go_sha256=$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' |
        jq -er --arg version "go$go_version" --arg archive "$go_archive" '
            .[] | select(.version == $version) | .files[]
            | select(.filename == $archive) | .sha256
            | select(test("^[a-f0-9]{64}$"))
        ')
    curl -fsSL "https://go.dev/dl/$go_archive" -o "$scratch/$go_archive"
    (
        cd "$scratch"
        printf '%s  %s\n' "$go_sha256" "$go_archive" | sha256sum --check
    )
    tar -xzf "$scratch/$go_archive" -C "$scratch"
    mv -T -- "$scratch/go" "$go_root"
fi
export GOTOOLCHAIN=local
"$go_root/bin/go" version
ln -sfn -- "$go_root/bin/go" "$HOME/.local/bin/go"
ln -sfn -- "$go_root/bin/gofmt" "$HOME/.local/bin/gofmt"

printf 'Installing Node 24 LTS, Python 3.14 and uv with mise\n'
mise="$HOME/.local/bin/mise"
if [[ ! -x $mise ]]; then
    curl -fsSL https://mise.run -o "$scratch/install-mise.sh"
    MISE_INSTALL_PATH="$mise" sh "$scratch/install-mise.sh"
fi
"$mise" use --global node@24 python@3.14 uv
"$mise" --version
"$mise" exec -- node --version
"$mise" exec -- python --version
"$mise" exec -- uv --version

printf 'Installing stable Rust with rustup\n'
rustup="${CARGO_HOME:-$HOME/.cargo}/bin/rustup"
if [[ ! -x $rustup ]]; then
    curl -fsSL https://sh.rustup.rs -o "$scratch/install-rust.sh"
    sh "$scratch/install-rust.sh" -y --profile minimal --default-toolchain stable --no-modify-path
else
    "$rustup" toolchain install stable --profile minimal
fi
"$rustup" --version
"$rustup" run stable rustc --version
"$rustup" run stable cargo --version

# Expand these paths when the login shell reads .profile.
# shellcheck disable=SC2016
profile_line='export PATH="$HOME/.local/bin:${MISE_DATA_DIR:-$HOME/.local/share/mise}/shims:${CARGO_HOME:-$HOME/.cargo}/bin:$PATH"'
if ! grep -Fxq -- "$profile_line" "$HOME/.profile" 2>/dev/null; then
    printf '\n%s\n' "$profile_line" >> "$HOME/.profile"
fi

printf 'Installing Docker Engine from the Docker apt repository\n'
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o "$scratch/docker.asc"
sudo install -d -m 0755 /etc/apt/keyrings
sudo install -m 0644 "$scratch/docker.asc" /etc/apt/keyrings/docker.asc
sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<DOCKER
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: ${UBUNTU_CODENAME:-$VERSION_CODENAME}
Components: stable
Architectures: $arch
Signed-By: /etc/apt/keyrings/docker.asc
DOCKER
sudo apt-get update
sudo apt-get install -y "${docker_packages[@]}"
sudo usermod -aG docker "$owner"
dpkg-query -W "${docker_packages[@]}"
sudo docker version
docker buildx version
docker compose version
printf 'Sign out and back in to activate your new groups and PATH.\n'
