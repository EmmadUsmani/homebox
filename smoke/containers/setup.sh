#!/usr/bin/env bash
set -euo pipefail

docker info >/dev/null
if [[ ${SMOKE_LEVEL:-full} == full ]]; then
    sudo -n timeout 180 apt-get update
    sudo -n env DEBIAN_FRONTEND=noninteractive timeout 300 apt-get install -y --no-install-recommends \
        podman uidmap passt fuse-overlayfs qemu-user-binfmt

    user=$(id -un)
    subid_start=$(awk -F: '
        BEGIN { next_id = 100000 }
        $2 + $3 > next_id { next_id = $2 + $3 }
        END { print next_id }
    ' /etc/subuid /etc/subgid)
    subid_range="$subid_start-$((subid_start + 65535))"
    subid_options=()
    getsubids "$user" >/dev/null 2>&1 || subid_options+=(--add-subuids "$subid_range")
    getsubids -g "$user" >/dev/null 2>&1 || subid_options+=(--add-subgids "$subid_range")
    if (( ${#subid_options[@]} )); then
        sudo -n usermod "${subid_options[@]}" "$user"
    fi

    tools="$HOME/.cache/homebox-smoke/containers"
    mkdir -p "$tools/bin"
    case "$(uname -m)" in
        aarch64) arch=arm64 ;;
        x86_64) arch=amd64 ;;
        *) echo "Unsupported Kubernetes tool architecture" >&2; exit 1 ;;
    esac

    install_tool() (
        set -e
        temporary=$(mktemp -d)
        trap 'rm -rf "$temporary"' EXIT
        cd "$temporary"
        curl --fail --silent --show-error --location --max-time 180 "$2" -o binary
        curl --fail --silent --show-error --location --max-time 60 "$2.$3" -o checksum
        printf '%s  binary\n' "$(awk 'NR==1 {print $1}' checksum)" | sha256sum -c -
        install -m 0755 binary "$tools/bin/$1"
    )
    install_tool kind "https://github.com/kubernetes-sigs/kind/releases/download/v0.27.0/kind-linux-$arch" sha256sum
    install_tool kubectl "https://dl.k8s.io/release/v1.32.2/bin/linux/$arch/kubectl" sha256

    cp fixtures/testcontainers/package*.json "$tools/"
    timeout 240 npm --prefix "$tools" ci --ignore-scripts --no-audit --no-fund
fi
