#!/usr/bin/env bash
# shellcheck disable=SC2016 # Commands expand on the guest or in the child shell.

set -eu
# shellcheck source=../lib.sh
. ../lib.sh
scratch=$(mktemp -d "$HOME/.homebox-smoke-os.XXXXXX")
export scratch
cleanup() {
    sudo -n rm -f /etc/cron.d/homebox-smoke-os
    rm -rf "$scratch"
}
trap cleanup EXIT

run() {
    check "$1" timeout "$2" bash -euo pipefail -c "$3"
}

run 'systemd system service restarts after a failure and journals its output' 20 '
    unit=homebox-smoke-restart-$$.service
    sudo -n systemd-run --unit="$unit" --wait --collect -p Restart=on-failure -p RestartSec=1s \
        /bin/sh -c '\''if test -f "$1"; then touch "$1.done"; else touch "$1"; exit 1; fi'\'' sh "$scratch/restart"
    test -f "$scratch/restart.done"
    journalctl -u "$unit" --no-pager -o cat | grep -F "Scheduled restart job"
'
run 'systemd user service can be enabled and restarts after a failure' 20 '
    unit=homebox-smoke-user-$$.service
    trap "systemctl --user disable --now $unit >/dev/null 2>&1 || true" EXIT
    cat >"$scratch/user-job" <<JOB
if test -f "$scratch/user-restart"; then
    touch "$scratch/user-done"
else
    touch "$scratch/user-restart"
    exit 1
fi
JOB
    cat >"$scratch/$unit" <<UNIT
[Service]
ExecStart=/bin/sh $scratch/user-job
Type=oneshot
RemainAfterExit=yes
Restart=on-failure
RestartSec=1s
[Install]
WantedBy=default.target
UNIT
    systemctl --user enable "$scratch/$unit"
    systemctl --user is-enabled "$unit"
    systemctl --user start --no-block "$unit"
    until test -f "$scratch/user-done" && systemctl --user is-active --quiet "$unit"; do
        sleep 1
    done
    test "$(systemctl --user show -p NRestarts --value "$unit")" -eq 1
    journalctl --user -u "$unit" --no-pager -o cat | grep -F "Scheduled restart job"
'
run 'systemd user timer runs a deferred job' 20 '
    systemd-run --user --on-active=1s --timer-property=AccuracySec=100ms --collect /usr/bin/touch "$scratch/timer"
    until test -f "$scratch/timer"; do sleep 1; done
'
run 'cron executes a scheduled job' 85 '
    systemctl is-active cron
    printf "* * * * * %s /usr/bin/touch %s/cron\n" "$USER" "$scratch" |
      sudo -n tee /etc/cron.d/homebox-smoke-os >/dev/null
    until test -f "$scratch/cron"; do sleep 1; done
'
run 'inotify watches 10,000 files and receives a write event' 25 '
    mkdir "$scratch/watch"
    for i in {1..10000}; do : > "$scratch/watch/$i"; done
    printf "%s\n" "$scratch"/watch/* > "$scratch/watches"
    inotifywait -e modify --fromfile "$scratch/watches" >"$scratch/event" 2>"$scratch/watchlog" &
    watcher=$!
    trap "kill $watcher 2>/dev/null || true" EXIT
    until grep -q "Watches established" "$scratch/watchlog"; do kill -0 "$watcher"; sleep 0.2; done
    echo smoke > "$scratch/watch/10000"
    wait "$watcher"
    grep -F "/10000 MODIFY" "$scratch/event"
'
run 'open-file and process limits support a development workload' 10 '
    test "$(ulimit -Hn)" -ge 16384
    limit=$(ulimit -u)
    [[ $limit == unlimited || $limit -ge 1024 ]]
    ulimit -Sn 4096
    python3 - <<PYTHON
import subprocess

with open("/dev/null") as null:
    children = [subprocess.Popen(["/bin/true"], stdout=null) for _ in range(64)]
    assert all(child.wait() == 0 for child in children)
files = [open("/dev/null") for _ in range(2048)]
PYTHON
'
run '128 MiB direct disk write and read complete with correct contents' 30 '
    dd if=/dev/zero of="$scratch/disk" bs=1M count=128 oflag=direct conv=fsync 2>"$scratch/throughput"
    # Ubuntu 26.04 uutils dd rejects direct reads. Use a page-aligned mmap buffer.
    python3 - "$scratch/disk" >>"$scratch/throughput" <<PYTHON
import mmap
import os
import sys
import time

size = 1024**2
expected = bytes(size)
with mmap.mmap(-1, size) as buffer:
    fd = os.open(sys.argv[1], os.O_RDONLY | os.O_DIRECT)
    start = time.monotonic()
    try:
        for _ in range(128):
            assert os.readv(fd, [buffer]) == size
            assert buffer[:] == expected
        assert os.readv(fd, [buffer]) == 0
    finally:
        os.close(fd)
    print(f"Verified direct read: {128 / (time.monotonic() - start):.0f} MiB/s")
PYTHON
    rm "$scratch/disk"
'
if [ -f "$scratch/throughput" ]; then cat "$scratch/throughput"; fi
run 'memory cgroup OOM kills a child while the guest stays responsive' 25 '
    unit=homebox-smoke-oom
    trap "sudo -n systemctl reset-failed $unit.service || true" EXIT
    if sudo -n systemd-run --unit="$unit" --wait -p MemoryMax=64M -p MemorySwapMax=0 \
        /usr/bin/python3 -c "data = bytearray(128 * 1024**2)"; then
        exit 1
    fi
    test "$(systemctl show "$unit" -p Result --value)" = oom-kill
    test -r /proc/meminfo
'
run 'swap can be enabled, observed and disabled' 25 '
    trap '\''sudo -n swapoff "$scratch/swap" || true'\'' EXIT
    install -m 600 /dev/null "$scratch/swap"
    dd if=/dev/zero of="$scratch/swap" bs=1M count=64 status=none
    sudo -n chown root:root "$scratch/swap"

    sudo -n mkswap "$scratch/swap"
    sudo -n swapon "$scratch/swap"
    swapon --show=NAME --noheadings | grep -Fx "$scratch/swap"
    sudo -n swapoff "$scratch/swap"
    trap - EXIT
'
run 'UTC timezone and UTF-8 locale preserve an emoji filename' 10 '
    test "$(timedatectl show -p Timezone --value)" = Etc/UTC
    test "$(locale charmap)" = UTF-8
    printf "سلام\n" >"$scratch/🧪.txt"
    grep -Fx "سلام" "$scratch/🧪.txt"
'
run 'local user and group membership are usable through sudo' 10 '
    user=homebox-smoke-os
    sudo -n groupadd "$user"
    trap '\''sudo -n userdel "$user" 2>/dev/null || true; sudo -n groupdel "$user" 2>/dev/null || true'\'' EXIT
    sudo -n useradd -M -g "$user" "$user"
    test "$(sudo -n -u "$user" id -gn)" = "$user"
    test "$(sudo -n id -u)" = 0
'
run 'guest clock synchronization is active' 45 '
    until [[ $(timedatectl show -p NTPSynchronized --value) == yes ]]; do sleep 1; done
'
finish
