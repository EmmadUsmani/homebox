#!/bin/sh
set -eu
export LC_ALL=C

gateway=10.114.0.1
ssh_gateway=192.168.5.2
gateway_mac=52:55:0a:72:00:15
cidata=/Volumes/cidata/lima.env
user=$(sed -n 's/^LIMA_CIDATA_USER=//p' "$cidata")
boot_id=$(sed -n 's/^LIMA_CIDATA_IID=//p' "$cidata")
test -n "$boot_id"
case "$user" in
    ''|*[!a-z0-9_-]*) exit 1 ;;
esac

# The read-only config disk describes this boot and the authorized account.
# Match interfaces independently of Apple's display names and device numbering.
egress=$(ifconfig -a | awk -v mac="$gateway_mac" '
    /^[^[:space:]]/ { split($1, fields, ":"); device = fields[1] }
    $1 == "ether" && $2 == mac { print device; exit }')
test -n "$egress"
service_for() {
    networksetup -listnetworkserviceorder | awk -v device="$1" '
        /^\([0-9]+\) / { name = substr($0, index($0, ") ") + 2) }
        index($0, "Device: " device ")") { print name; exit }'
}
route_field() {
    route -n get "$1" 2>/dev/null | awk -v key="$2:" '$1 == key { print $2; exit }'
}

attempt=0
while :; do
    inbound=$(route_field "$ssh_gateway" interface)
    if [ -n "$inbound" ] && [ "$inbound" != "$egress" ] &&
        [ "$(ipconfig getifaddr "$inbound" 2>/dev/null || true)" = 192.168.5.15 ]; then
        break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 30 ]; then
        echo 'The Lima SSH interface did not receive its expected address' >&2
        exit 1
    fi
    sleep 1
done

inbound_service=$(service_for "$inbound")
egress_service=$(service_for "$egress")
test -n "$inbound_service"
if [ -z "$egress_service" ]; then
    networksetup -createnetworkservice 'Homebox Egress' "$egress"
    egress_service=$(service_for "$egress")
fi
test -n "$egress_service"
networksetup -setmanual "$inbound_service" 192.168.5.15 255.255.255.0 0.0.0.0
networksetup -setdnsservers "$inbound_service" Empty
networksetup -setsearchdomains "$inbound_service" Empty
networksetup -setv6off "$inbound_service"
networksetup -setdhcp "$egress_service"
networksetup -setdnsservers "$egress_service" 9.9.9.9 1.1.1.1
networksetup -setsearchdomains "$egress_service" Empty
networksetup -setv6off "$egress_service"

attempt=0
until [ "$(route_field default gateway)" = "$gateway" ] &&
    [ "$(route_field default interface)" = "$egress" ]; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 30 ]; then
        echo 'The homebox gateway did not become the default route' >&2
        exit 1
    fi
    sleep 1
done
netstat -rn -f inet | awk -v gateway="$gateway" '
    $1 == "default" && $2 != gateway { bad = 1 }
    END { exit bad }'

install -d -m 0755 /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/10-homebox.conf <<'SSH'
AllowAgentForwarding no
X11Forwarding no
PasswordAuthentication no
KbdInteractiveAuthentication no
SSH
/usr/sbin/sshd -t

install -d -m 0755 /etc/sudoers.d
sudoers=$(mktemp /etc/homebox-sudoers.XXXXXX)
trap 'rm -f "$sudoers"' EXIT
printf '%s ALL=(ALL) NOPASSWD: ALL\n' "$user" > "$sudoers"
chmod 0440 "$sudoers"
visudo -cf "$sudoers"
mv -f "$sudoers" /etc/sudoers.d/100-homebox
sudo -u "$user" /usr/bin/sudo -n /usr/bin/true
printf '%s\n' "$boot_id" > /var/run/homebox-network-ready
