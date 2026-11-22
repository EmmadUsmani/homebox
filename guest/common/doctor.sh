#!/bin/sh
set -eu
set -f

public=${HOMEBOX_PUBLIC_URL:-https://example.com}
failed=0
pass() { printf 'PASS %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2"; failed=1; }
# Positive checks cross the internet, so give a slow moment a second chance.
retry() {
    "$@" || { sleep 1; "$@"; } || { sleep 2; "$@"; }
}

if retry curl -q -4 -fsS --noproxy '*' -m 15 -o /dev/null "$public"; then
    pass public-tcp
else
    fail public-tcp 'public HTTPS request failed'
fi
if retry getent ahostsv4 example.com >/dev/null; then
    pass dns
else
    fail dns 'system resolver could not resolve example.com'
fi
# Called through retry below.
# shellcheck disable=SC2329
udp_dns() {
    dig +time=2 +tries=2 +short @9.9.9.9 example.com A | grep -Eq '^[0-9]+\.'
}
if retry udp_dns; then
    pass public-udp-dns
else
    fail public-udp-dns 'public DNS over UDP failed'
fi
if retry ping -4 -c 2 -W 2 1.1.1.1 >/dev/null; then
    pass public-icmp
else
    fail public-icmp 'public IPv4 echo failed'
fi

if ! python3 - <<'PYTHON'
import concurrent.futures
import os
import socket
import struct
import subprocess
import time

def ntp():
    request = bytearray(48)
    request[0] = 0x23
    struct.pack_into("!Q", request, 40, int((time.time() + 2208988800) * 2**32))
    for name in ("time.cloudflare.com", "time.google.com") * 2:
        try:
            address = socket.getaddrinfo(name, 123, socket.AF_INET, socket.SOCK_DGRAM)[0][4]
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
                connection.settimeout(3)
                connection.connect(address)
                connection.send(request)
                reply = connection.recv(512)
                if len(reply) >= 48 and reply[0] & 7 == 4 and reply[24:32] == request[40:48]:
                    return "public-udp-ntp", True
        except OSError:
            pass
    return "public-udp-ntp", False

def blocked(protocol, target):
    host, port = target.split(":")
    if protocol == "icmp":
        refused = subprocess.run(
            ["ping", "-4", "-c", "1", "-W", "1", host],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        ).returncode == 1
    else:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM if protocol == "tcp" else socket.SOCK_DGRAM) as conn:
            conn.settimeout(1)
            try:
                conn.connect((host, int(port)))
                if protocol == "udp":
                    conn.send(b"homebox doctor")
                    conn.recv(128)
                refused = False
            except OSError:
                refused = True
    return f"deny-{protocol}-{target}", refused

builtin = os.getenv("HOMEBOX_BUILTIN_GATEWAY", "192.168.5.2")
targets = os.getenv("HOMEBOX_DENY_TARGETS", "").split()
loopback_ports = [target.split(":")[1] for target in targets if target.startswith(builtin + ":")]
targets += [f"10.114.0.1:{port}" for port in loopback_ports or ["80"]]
targets += ["10.100.50.1:80", "172.30.250.1:80", "192.168.250.1:80",
            "100.64.3.1:80", "169.254.169.254:80"]
with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
    results = [pool.submit(ntp)]
    for target in targets:
        for protocol in ("tcp", "udp", "icmp"):
            # Lima itself answers echo at its virtual gateway.
            if protocol != "icmp" or target.split(":")[0] != builtin:
                results.append(pool.submit(blocked, protocol, target))
    if builtin and any(target.startswith(builtin + ":") for target in targets):
        print("SKIP deny-icmp-built-in-gateway: Lima answers locally")
    failed = False
    for result in results:
        label, ok = result.result()
        print(f"{'PASS' if ok else 'FAIL'} {label}")
        failed |= not ok
    raise SystemExit(int(failed))
PYTHON
then
    failed=1
fi

mount=
while read -r _source destination type _rest; do
    case "$type" in
        virtiofs|9p|sshfs|fuse.sshfs)
            mount=$destination
            break
            ;;
    esac
done < /proc/mounts
if [ -n "$mount" ]; then
    fail no-host-mounts "found $type at $mount"
else
    pass no-host-mounts
fi

if [ "${SSH_AUTH_SOCK+x}" ]; then
    fail no-ssh-agent 'SSH_AUTH_SOCK is set'
else
    pass no-ssh-agent
fi
exit "$failed"
