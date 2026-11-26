#!/usr/bin/env bash
set -euo pipefail

if (( EUID != 0 )); then
    echo 'Run the guest credential scan as root.' >&2
    exit 2
fi

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
roots=(/home /root /etc /tmp /var/tmp /var/lib /opt /usr/local /srv)
if [[ -d /nix/store ]]; then roots+=(/nix/store); fi
find / "${roots[@]}" -xdev \( -path /proc -o -path /sys -o -path /dev -o -path /run \) -prune -o \
    \( -type f -o -type l \) \( -name '.env*' -o -name '.netrc' -o -name '.git-credentials' \
    -o -path '*/.ssh/id_*' -o -path '/etc/ssl/private/*' \
    -o -path '*/.aws/credentials' -o -path '*/.config/gh/hosts.yml' \
    -o -path '*/.config/gcloud/application_default_credentials.json' -o -path '*/.kube/config' \
    -o -path '*/.codex/auth.json' -o -name '.credentials.json' \
    -o -path '*/tailscale/tailscaled.state' \) -print > "$work/findings"

# Scan the guest's writable state and configuration. grep prints filenames
# only, including for a match in shell history or a JSON credential file.
pattern='^[[:space:]]*-----BEGIN OPENSSH PRIVATE KEY-----[[:space:]]*$|-----BEGIN OPENSSH PRIVATE KEY-----\\nb3BlbnNzaC1rZXkt|gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{50,}|(^|[^A-Za-z0-9_-])sk-((ant-|proj-)[A-Za-z0-9_-]{32,}|[A-Za-z0-9]{48})([^A-Za-z0-9_-]|$)|(^|[^A-Za-z0-9_])(AKIA|ASIA)[A-Z0-9]{16}([^A-Za-z0-9_]|$)|xox[baprs]-[A-Za-z0-9-]{20,}|tskey-(auth|api|client)-[A-Za-z0-9_-]{20,}'
find "${roots[@]}" -xdev -path "$work" -prune -o -type f \
    ! -name '.env*' \
    ! -path /etc/ssh/ssh_host_rsa_key \
    ! -path /etc/ssh/ssh_host_ecdsa_key \
    ! -path /etc/ssh/ssh_host_ed25519_key \
    -exec bash -c '
        pattern=$1
        shift
        status=0
        LC_ALL=C grep -IlE -- "$pattern" "$@" || status=$?
        (( status <= 1 ))
    ' bash "$pattern" {} + >> "$work/findings"

if [[ -s "$work/findings" ]]; then
    echo 'Guest credential scan found these paths:' >&2
    sort -u "$work/findings" >&2
    exit 1
fi
echo 'Guest credential scan passed.'
