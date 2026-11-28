#!/usr/bin/env bash
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"

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
