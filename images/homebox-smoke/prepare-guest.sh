#!/usr/bin/env bash
set -euo pipefail

# Published ssh2 packages contain public test private keys. Remove the
# fixtures from the installed packages and their pnpm content store.
store="$HOME/.local/share/pnpm/store"
find "$HOME" -xdev -type d -path '*/node_modules/ssh2/test/fixtures' -prune -print0 |
while IFS= read -r -d '' fixtures; do
    if [[ -d "$store" ]]; then
        find "$fixtures" -type f -print0 | while IFS= read -r -d '' file; do
            hash=$(sha512sum < "$file")
            hash=${hash%% *}
            for version in "$store"/*; do
                rm -f -- "$version/files/${hash:0:2}/${hash:2}" "$version/files/${hash:0:2}/${hash:2}-exec"
            done
        done
    fi
    rm -rf -- "$fixtures"
done
if [[ -d "$store" ]]; then
    find "$store" -type f -path '*/index/*/*-ssh2@*.json' -delete
fi

# The services smoke virtualenv ships AWS example credentials in SDK docs.
for packages in "$HOME/homebox-smoke/services/.venv/lib/"python*/site-packages; do
    rm -f -- "$packages/boto3/examples/cloudfront.rst" \
        "$packages/botocore/data/iam/2010-05-08/examples-1.json" \
        "$packages/botocore/data/sts/2011-06-15/examples-1.json"
done

# Preserve the built Nix packages. Only the fetched, unreferenced Nixpkgs
# source tree contains NixOS SSH test fixtures. Pass the script on stdin
# because Nix treats store paths in sudo's SUDO_COMMAND as live roots.
sudo -n bash -s <<'NIX'
set -eu
for nixos in /nix/store/*-source/nixos; do
    [[ -d "$nixos/tests" && -d "${nixos%/nixos}/pkgs" ]] || continue
    source=${nixos%/nixos}
    referrers=$(nix-store --query --referrers "$source")
    if [[ -z "$referrers" ]]; then
        nix-store --delete "$source"
    fi
done
NIX
