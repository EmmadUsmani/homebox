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
import json
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

def blocked(protocol, target, device=None):
    host, port = target.split(":")
    if protocol == "icmp":
        refused = subprocess.run(
            ["ping", "-4", "-c", "1", "-W", "1"] + (["-I", device] if device else []) + [host],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        ).returncode == 1
    else:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM if protocol == "tcp" else socket.SOCK_DGRAM) as conn:
            if device:
                conn.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, device.encode() + b"\0")
            conn.settimeout(1)
            try:
                conn.connect((host, int(port)))
                if protocol == "udp":
                    if host == "1.1.1.1" and port == "53":
                        conn.send(bytes.fromhex("123401000001000000000000076578616d706c6503636f6d0000010001"))
                    else:
                        conn.send(b"homebox doctor")
                    conn.recv(128)
                refused = False
            except OSError:
                refused = True
    return f"deny-{protocol}-{target}", refused

builtin = os.getenv("HOMEBOX_BUILTIN_GATEWAY", "192.168.5.2")
targets = os.getenv("HOMEBOX_DENY_TARGETS", "").split()
loopback_ports = [target.split(":")[1] for target in targets if target.startswith(builtin + ":")]
gateway = os.environ["HOMEBOX_GATEWAY_IP"]
targets += [f"{gateway}:{port}" for port in loopback_ports or ["80"]]
targets += ["10.100.50.1:80", "172.30.250.1:80", "192.168.250.1:80",
            "100.64.3.1:80", "169.254.169.254:80"]
def route_to(address):
    return json.loads(subprocess.check_output(["ip", "-j", "-4", "route", "get", address]))[0]

def check_slirp():
    route = route_to(builtin)
    device = route["dev"]
    if route.get("gateway") or device == "lo":
        raise RuntimeError("built-in gateway is not directly attached")
    # Test the built-in path explicitly even while normal traffic uses Homebox.
    slirp_targets = targets + ["1.1.1.1:443", "1.1.1.1:53"]
    added = []
    try:
        for address in sorted({target.split(":")[0] for target in slirp_targets} - {builtin}):
            command = ["sudo", "-n", "ip", "-4", "route"]
            arguments = [address + "/32", "via", builtin, "dev", device]
            subprocess.run(command + ["add"] + arguments, check=True)
            added.append(command + ["del"] + arguments)
        with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
            results = [pool.submit(blocked, protocol, target, device)
                       for target in slirp_targets for protocol in ("tcp", "udp", "icmp")
                       if protocol != "icmp" or target.split(":")[0] != builtin]
            failed = False
            for result in results:
                label, ok = result.result()
                print(f"{'PASS' if ok else 'FAIL'} slirp-{label}")
                failed |= not ok
            return failed
    finally:
        for command in reversed(added):
            subprocess.run(command, check=True)

with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
    results = [pool.submit(ntp)]
    # A host bridge can use the same address as a guest bridge. Bind each
    # canary probe to the VM's actual uplink so it cannot hit the guest bridge.
    egress_device = route_to("1.1.1.1")["dev"]
    inbound_device = route_to(builtin)["dev"]
    for target in targets:
        for protocol in ("tcp", "udp", "icmp"):
            # Lima itself answers echo at its virtual gateway.
            if protocol != "icmp" or target.split(":")[0] != builtin:
                device = inbound_device if target.split(":")[0] == builtin else egress_device
                results.append(pool.submit(blocked, protocol, target, device))
    if builtin and any(target.startswith(builtin + ":") for target in targets):
        print("SKIP deny-icmp-built-in-gateway: Lima answers locally")
    failed = False
    for result in results:
        label, ok = result.result()
        print(f"{'PASS' if ok else 'FAIL'} {label}")
        failed |= not ok
if os.getenv("HOMEBOX_CHECK_SLIRP") == "1":
    try:
        failed |= check_slirp()
    except (OSError, KeyError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"FAIL slirp-route: {error}")
        failed = True
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
