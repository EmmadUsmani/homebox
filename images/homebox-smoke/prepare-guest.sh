#!/usr/bin/env bash
set -euo pipefail

# Status commands can return nonzero for a signed-out agent. Require an
# explicit signed-out response and keep the full responses out of build logs.
status=0
claude_status=$(claude auth status 2>&1) || status=$?
if (( status > 1 )) || ! jq -e 'type == "object" and .loggedIn == false' <<<"$claude_status" >/dev/null 2>&1; then
    echo 'Claude Code did not report a signed-out session.' >&2
    exit 1
fi
status=0
codex_status=$(codex login status 2>&1) || status=$?
if (( status > 1 )) || [[ "$codex_status" != 'Not logged in' ]]; then
    echo 'Codex did not report a signed-out session.' >&2
    exit 1
fi
echo 'Claude Code and Codex are signed out.'

# The Tailscale package starts briefly during installation and can leave
# an empty state file. Leave any populated state for the scanner to reject.
tailscale_state=/var/lib/tailscale/tailscaled.state
if sudo -n jq -e 'type == "object" and length == 0' "$tailscale_state" >/dev/null 2>&1; then
    sudo -n rm -- "$tailscale_state"
fi

# PostgreSQL uses Ubuntu's generated TLS key. Order startup after ssl-cert
# so each restored guest has its own key before PostgreSQL reads it.
snakeoil_key=/etc/ssl/private/ssl-cert-snakeoil.key
if sudo -n test -f "$snakeoil_key"; then
    sudo -n mkdir -p /etc/systemd/system/postgresql@.service.d
    sudo -n tee /etc/systemd/system/postgresql@.service.d/homebox-ssl-cert.conf >/dev/null <<'UNIT'
[Unit]
Wants=ssl-cert.service
After=ssl-cert.service
UNIT
    sudo -n rm -- "$snakeoil_key" /etc/ssl/certs/ssl-cert-snakeoil.pem
fi

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
