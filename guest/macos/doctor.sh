#!/bin/sh
set -eu
set -f

builtin=$HOMEBOX_BUILTIN_GATEWAY
gateway=$HOMEBOX_GATEWAY_IP
failed=0
pass() { printf 'PASS %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2"; failed=1; }
retry() { "$@" || { sleep 1; "$@"; }; }
route_field() {
    field=$1
    shift
    route -n get "$@" 2>/dev/null | awk -v key="$field:" '$1 == key { print $2; exit }'
}
if [ "$(route_field gateway default)" != "$gateway" ]; then
    fail socket-default-route 'the default route does not use the homebox gateway'
    exit 1
fi
egress=$(route_field interface default)
inbound=$(route_field interface "$builtin")
if [ -z "$egress" ] || [ -z "$inbound" ] || [ "$egress" = "$inbound" ]; then
    fail network-interfaces 'the SSH and egress routes must use separate interfaces'
    exit 1
fi
if netstat -rn -f inet | awk -v gateway="$gateway" '$1 == "default" && $2 != gateway { bad = 1 } END { exit !bad }'; then
    fail socket-default-route 'a second default route is present'
else
    pass socket-default-route
fi

if retry curl -q -4 -fsS --noproxy '*' -m 15 -o /dev/null "$HOMEBOX_PUBLIC_URL"; then
    pass public-tcp
else
    fail public-tcp 'public HTTPS request failed'
fi
# Called through retry.
# shellcheck disable=SC2329
system_dns() {
    dscacheutil -q host -a name example.com | grep -q '^ip_address:'
}
if retry system_dns; then
    pass dns
else
    fail dns 'the system resolver could not resolve example.com'
fi
# Called through retry.
# shellcheck disable=SC2329
public_udp_dns() {
    dig -4 +time=2 +tries=1 +short @9.9.9.9 example.com A | grep -Eq '^[0-9]+\.'
}
if retry public_udp_dns; then
    pass public-udp-dns
else
    fail public-udp-dns 'public DNS over UDP failed'
fi
if sntp -t 3 time.cloudflare.com >/dev/null 2>&1 ||
    sntp -t 3 time.google.com >/dev/null 2>&1; then
    pass public-udp-ntp
else
    fail public-udp-ntp 'public NTP over UDP failed'
fi
if retry ping -b "$egress" -n -c 1 -W 1000 -t 2 1.1.1.1 >/dev/null; then
    pass public-icmp
else
    fail public-icmp 'public IPv4 echo failed'
fi

# Test the SSH interface with explicit routes, as guest root can select it.
# The interface-bound probes ensure failed routes cannot fall back to egress.
route_target=
trap 'if [ -n "$route_target" ]; then route -n delete -host -ifscope "$inbound" "$route_target" "$builtin" >/dev/null; fi' EXIT
probe_denials() {
    target=$1
    nic=$2
    prefix=$3
    addr=${target%:*}
    port=${target##*:}
    if nc -4 -b "$nic" -z -G 1 -w 1 "$addr" "$port" >/dev/null 2>&1; then
        fail "$prefix-deny-tcp-$target" 'opened a connection'
    else
        pass "$prefix-deny-tcp-$target"
    fi
    if [ "$target" = 1.1.1.1:53 ]; then
        udp=$(printf '\022\064\001\000\000\001\000\000\000\000\000\000\007example\003com\000\000\001\000\001' |
            nc -4 -b "$nic" -u -w 1 "$addr" "$port" 2>/dev/null | wc -c)
    else
        udp=$(printf 'homebox doctor' | nc -4 -b "$nic" -u -w 1 "$addr" "$port" 2>/dev/null | wc -c)
    fi
    if [ "$udp" -gt 0 ]; then
        fail "$prefix-deny-udp-$target" 'received a response'
    else
        pass "$prefix-deny-udp-$target"
    fi
    if [ "$addr" = "$builtin" ]; then
        printf 'SKIP %s-deny-icmp-%s: Lima answers its own gateway locally\n' "$prefix" "$addr"
    elif ping -b "$nic" -n -c 1 -W 1000 -t 2 "$addr" >/dev/null 2>&1; then
        fail "$prefix-deny-icmp-$target" 'received an echo reply'
    else
        pass "$prefix-deny-icmp-$target"
    fi
}

for target in $HOMEBOX_DENY_TARGETS; do
    case "$target" in
        "$builtin":*) probe_denials "$gateway:${target##*:}" "$egress" gateway ;;
        *) probe_denials "$target" "$egress" gateway ;;
    esac
done
for target in $HOMEBOX_DENY_TARGETS 1.1.1.1:443 1.1.1.1:53; do
    addr=${target%:*}
    if [ "$addr" != "$builtin" ]; then
        if ! route -n add -host -ifscope "$inbound" "$addr" "$builtin" >/dev/null; then
            fail "slirp-route-$addr" 'could not install the probe route'
            continue
        fi
        route_target=$addr
        if [ "$(route_field gateway -ifscope "$inbound" "$addr")" != "$builtin" ]; then
            fail "slirp-route-$addr" 'the probe route did not select the Lima gateway'
        fi
    fi
    probe_denials "$target" "$inbound" slirp
    if [ -n "$route_target" ]; then
        route -n delete -host -ifscope "$inbound" "$route_target" "$builtin" >/dev/null
        route_target=
    fi
done

if mount | grep -Eq '\((virtiofs|sshfs|9p)[,)]'; then
    fail no-host-mounts 'found a host filesystem mount'
else
    pass no-host-mounts
fi
if [ "${SSH_AUTH_SOCK+x}" ]; then
    fail no-ssh-agent 'SSH_AUTH_SOCK is set'
else
    pass no-ssh-agent
fi
exit "$failed"
