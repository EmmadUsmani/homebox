#!/bin/sh
# shellcheck disable=SC2016 # Commands expand on the guest or in the child shell.

set -eu
# shellcheck source=../lib.sh
. "$(dirname "$0")/../lib.sh"

remote() { "$BOX" ssh "$SMOKE_VM" -- "$@"; }
cleanup() {
    remote 'tmux -L homebox-smoke kill-server 2>/dev/null || true
        sudo -n systemctl disable --now homebox-smoke-boot.service 2>/dev/null || true
        sudo -n rm -f /etc/systemd/system/homebox-smoke-boot.service
        sudo -n systemctl daemon-reload
        rm -f ~/homebox-smoke-persist' >/dev/null 2>&1 || true
}
trap cleanup EXIT

tmux_test() {
    remote 'tmux -L homebox-smoke new-session -d -s smoke' &&
        remote 'tmux -L homebox-smoke has-session -t smoke'
}
boot_test() {
    remote '
        set -eu
        cat <<EOF_UNIT | sudo -n tee /etc/systemd/system/homebox-smoke-boot.service >/dev/null
[Unit]
Description=Homebox smoke boot fixture
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/echo homebox boot smoke
[Install]
WantedBy=multi-user.target
EOF_UNIT
        echo survived > ~/homebox-smoke-persist
        sudo -n systemctl daemon-reload
        sudo -n systemctl enable homebox-smoke-boot.service
    ' &&
        "$BOX" stop "$SMOKE_VM" &&
        "$BOX" start "$SMOKE_VM" &&
        remote 'test "$(cat ~/homebox-smoke-persist)" = survived &&
            systemctl is-enabled homebox-smoke-boot.service &&
            systemctl is-active homebox-smoke-boot.service &&
            journalctl -b -u homebox-smoke-boot.service -o cat --no-pager | grep -Fx "homebox boot smoke"'
}

check 'tmux session survives disconnect and reconnect over SSH' tmux_test
check 'guest file and enabled service survive box stop and start' boot_test
finish
