#!/bin/sh
set -eu

stub=169.254.53.53
mac=52:55:0a:72:00:15
device=
for address in /sys/class/net/*/address; do
    if [ "$(cat "$address")" = "$mac" ]; then
        device=${address%/address}
        device=${device##*/}
        break
    fi
done
if [ -z "$device" ]; then
    echo 'Socket NIC is missing' >&2
    exit 1
fi
# The SSH NIC has Lima's built-in gateway; its name depends on the image.
builtin=$(ip -4 -o route get 192.168.5.2 | awk '{for (i=1; i<NF; i++) if ($i == "dev") print $(i+1)}')
if [ -z "$builtin" ] || [ "$builtin" = "$device" ]; then
    echo 'Could not identify the built-in SSH NIC' >&2
    exit 1
fi
resolved=/etc/systemd/resolved.conf.d/80-homebox.conf
builtin_network=$(LC_ALL=C networkctl status --no-pager "$builtin" | sed -n 's/^[[:space:]]*Network File: .*\/\([^/]*\.network\)$/\1/p')
if [ -z "$builtin_network" ]; then
    echo 'Could not identify the systemd network file for the built-in SSH NIC' >&2
    exit 1
fi
link=/etc/systemd/network/$builtin_network.d/80-homebox-dns.conf
loopback=/etc/systemd/network/05-homebox-dns.network
egress=/etc/systemd/network/05-homebox-egress.network

ip link show dev "$device" >/dev/null
mkdir -p "${resolved%/*}" "${link%/*}"
cat > "$link" <<'NETWORK'
[Network]
DNS=
Domains=
[DHCPv4]
UseDNS=no
UseDomains=no
[DHCPv6]
UseDNS=no
UseDomains=no
[IPv6AcceptRA]
UseDNS=no
UseDomains=no
NETWORK
cat > "$egress" <<NETWORK
[Match]
MACAddress=$mac

[Network]
DHCP=ipv4
IPv6AcceptRA=no
LinkLocalAddressing=no

[DHCPv4]
RouteMetric=100
UseDNS=no
UseDomains=no
NETWORK
cat > "$resolved" <<'RESOLVE'
[Resolve]
DNS=9.9.9.9 1.1.1.1
FallbackDNS=
Domains=~.
RESOLVE
rm -f "$loopback"
if ip -4 address show dev lo to "$stub/32" | grep -q 'inet '; then
    ip address del "$stub/32" dev lo
fi
networkctl reload
networkctl reconfigure "$device"
systemctl restart systemd-resolved.service
resolvectl revert "$builtin"
resolvectl default-route "$builtin" no

unit=/etc/systemd/system/homebox-redsocks.service
rules=/usr/local/sbin/homebox-tcp-redirect
if [ -f "$unit" ]; then
    systemctl disable --now homebox-redsocks.service
    if dpkg-query -W -f='${db:Status-Status}' redsocks 2>/dev/null | grep -qx installed; then
        apt-get purge -y redsocks
    fi
fi
if [ -x "$rules" ]; then
    "$rules" stop
fi
rm -f "$unit" "$rules" /etc/homebox-redsocks.conf
systemctl daemon-reload
if getent group homebox-direct >/dev/null; then
    groupdel homebox-direct
fi

# Preserve unrelated Docker daemon settings.
if python3 - "$stub" <<'PY'
import json
import sys
from pathlib import Path
from tempfile import NamedTemporaryFile

path = Path("/etc/docker/daemon.json")
config = json.loads(path.read_text()) if path.exists() else {}
if config.get("dns") != sys.argv[1:]:
    sys.exit(2)
del config["dns"]
if config:
    with NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as temporary:
        temporary.write(json.dumps(config, indent=2) + "\n")
    Path(temporary.name).replace(path)
else:
    path.unlink()
PY
then
    if systemctl is-active --quiet docker.service; then
        systemctl restart docker.service
    fi
else
    status=$?
    [ "$status" = 2 ] || exit "$status"
fi

for _attempt in 1 2 3 4 5 6 7 8 9 10; do
    if ip -4 route get 9.9.9.9 | grep -Fq " dev $device "; then
        exit 0
    fi
    sleep 1
done
echo 'Socket NIC did not become the IPv4 default route' >&2
exit 1
