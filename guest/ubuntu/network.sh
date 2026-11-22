#!/bin/sh
set -eu

dns=${HOMEBOX_DNS:-}
proxy=${HOMEBOX_PROXY_URL:-}
stub=169.254.53.53
resolved=/etc/systemd/resolved.conf.d/80-homebox.conf
link=/etc/systemd/network/10-netplan-eth0.network.d/80-homebox-dns.conf
loopback=/etc/systemd/network/05-homebox-dns.network

if [ -n "$dns" ]; then
    case "$dns" in
        *[!0-9.:]*|:*|*:|*:*:*) echo 'Invalid HOMEBOX_DNS endpoint' >&2; exit 1 ;;
        *:*) ;;
        *) echo 'HOMEBOX_DNS requires an explicit port' >&2; exit 1 ;;
    esac
    mkdir -p "${resolved%/*}" "${link%/*}"
    # The cloud-init netplan supplies a static DNS entry as well as DHCP.
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
    printf '[Match]\nName=lo\n[Network]\nAddress=%s/32\n' "$stub" > "$loopback"
    printf '[Resolve]\nDNS=%s\nFallbackDNS=\nDomains=~.\nDNSStubListenerExtra=%s\n' \
        "$dns" "$stub" > "$resolved"
elif [ -f "$resolved" ] || [ -f "$link" ] || [ -f "$loopback" ]; then
    rm -f "$link" "$loopback" "$resolved"
    if ip -4 address show dev lo to "$stub/32" | grep -q 'inet '; then
        ip address del "$stub/32" dev lo
    fi
fi
networkctl reload
systemctl restart systemd-resolved.service

# Docker's resolv.conf needs a reachable address on the standard DNS port.
if python3 - "$dns" "$stub" <<'PY'
import json
import sys
from pathlib import Path
from tempfile import NamedTemporaryFile

dns, stub = sys.argv[1:]
path = Path("/etc/docker/daemon.json")
config = json.loads(path.read_text()) if path.exists() else {}
if dns:
    if config.get("dns") == [stub]:
        sys.exit(2)
    config["dns"] = [stub]
elif config.get("dns") == [stub]:
    del config["dns"]
else:
    sys.exit(2)

if config:
    path.parent.mkdir(exist_ok=True)
    with NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as temporary:
        temporary.write(json.dumps(config, indent=2) + "\n")
    Path(temporary.name).replace(path)
else:
    path.unlink(missing_ok=True)
PY
then
    if systemctl is-active --quiet docker.service; then
        systemctl restart docker.service
    fi
else
    status=$?
    [ "$status" = 2 ] || exit "$status"
fi

unit=/etc/systemd/system/homebox-redsocks.service
rules=/usr/local/sbin/homebox-tcp-redirect
config=/etc/homebox-redsocks.conf
if [ -z "$proxy" ]; then
    if [ -f "$unit" ]; then
        systemctl disable --now homebox-redsocks.service
    fi
    if [ -x "$rules" ]; then
        "$rules" stop
    fi
    rm -f "$unit" "$rules" "$config"
    systemctl daemon-reload
    if getent group homebox-direct >/dev/null; then
        groupdel homebox-direct
    fi
    exit 0
fi

case "$proxy" in
    http://*:* ) ;;
    *) echo 'HOMEBOX_PROXY_URL must be http://IPv4:PORT' >&2; exit 1 ;;
esac
endpoint=${proxy#http://}
proxy_ip=${endpoint%:*}
proxy_port=${endpoint##*:}
case "$proxy_ip" in
    ''|*[!0-9.]*) echo 'Invalid proxy IPv4 address' >&2; exit 1 ;;
esac
case "$proxy_port" in
    ''|*[!0-9]*) echo 'Invalid proxy port' >&2; exit 1 ;;
esac

export DEBIAN_FRONTEND=noninteractive
if ! command -v redsocks >/dev/null || ! command -v iptables >/dev/null; then
    apt-get update
    apt-get install -y --no-install-recommends redsocks iptables
fi
systemctl disable --now redsocks.service
getent group homebox-direct >/dev/null || groupadd --system homebox-direct

# Remove live rules before replacing their configuration.
systemctl stop homebox-redsocks.service 2>/dev/null || [ ! -f "$unit" ]
port=12346
cat > "$config" <<REDSOCKS
base {
    log = "stderr";
    daemon = off;
    redirector = iptables;
}
redsocks {
    local_ip = 0.0.0.0;
    local_port = $port;
    ip = $proxy_ip;
    port = $proxy_port;
    type = http-connect;
}
REDSOCKS

cat > "$rules" <<'RULES'
#!/bin/sh
set -eu

nat() {
    iptables -w -t nat "$@"
}

remove_rules() {
    if ! nat -S HOMEBOX_TCP >/dev/null 2>&1; then
        return 0
    fi
    nat -D OUTPUT -o eth0 -p tcp -m owner ! --gid-owner homebox-direct --suppl-groups -j HOMEBOX_TCP 2>/dev/null || true
    for bridge in docker0 'br-+'; do
        nat -D PREROUTING -i "$bridge" -p tcp -j HOMEBOX_TCP 2>/dev/null || true
    done
    nat -F HOMEBOX_TCP
    nat -X HOMEBOX_TCP
}

remove_rules
if [ "$1" = stop ]; then
    exit 0
fi

nat -N HOMEBOX_TCP
nat -A HOMEBOX_TCP -d 127.0.0.0/8 -j RETURN
nat -A HOMEBOX_TCP -d 192.168.5.0/24 -j RETURN
nat -A HOMEBOX_TCP -d "$2" -p tcp --dport "$3" -j RETURN
nat -A HOMEBOX_TCP -m addrtype --dst-type LOCAL -j RETURN
nat -A HOMEBOX_TCP -p tcp -j REDIRECT --to-ports "$4"
nat -I OUTPUT -o eth0 -p tcp -m owner ! --gid-owner homebox-direct --suppl-groups -j HOMEBOX_TCP
for bridge in docker0 'br-+'; do
    nat -I PREROUTING -i "$bridge" -p tcp -j HOMEBOX_TCP
done
RULES
chmod 0755 "$rules"
cat > "$unit" <<UNIT
[Unit]
Description=Homebox transparent TCP proxy
Wants=network-online.target
After=network-online.target
Before=docker.service

[Service]
Type=exec
User=redsocks
NoNewPrivileges=yes
ExecStart=/usr/sbin/redsocks -c $config
ExecStartPost=+$rules start $proxy_ip $proxy_port $port
ExecStopPost=+$rules stop
Restart=on-failure
RestartSec=1

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now homebox-redsocks.service
