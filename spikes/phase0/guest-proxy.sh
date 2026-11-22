#!/bin/sh
# Run as root in the disposable Ubuntu 24.04 spike guest.
set -eu

proxy=http://192.168.5.2:23128
cat > /etc/apt/apt.conf.d/90-homebox-spike <<APT
Acquire::http::Proxy "$proxy";
Acquire::https::Proxy "$proxy";
APT

if [ "${1:-}" = transparent ]; then
    apt-get install -y redsocks
    cat > /etc/redsocks.conf <<'REDSOCKS'
base {
    log = "syslog:daemon";
    daemon = on;
    redirector = iptables;
}
redsocks {
    local_ip = 127.0.0.1;
    local_port = 12345;
    ip = 192.168.5.2;
    port = 23128;
    type = http-connect;
}
REDSOCKS
    systemctl restart redsocks

    for port in 22 80 443; do
        if ! iptables -t nat -C OUTPUT -p tcp --dport "$port" ! -d 127.0.0.0/8 -j REDIRECT --to-ports 12345 2>/dev/null; then
            iptables -t nat -A OUTPUT -p tcp --dport "$port" ! -d 127.0.0.0/8 -j REDIRECT --to-ports 12345
        fi
    done
fi
