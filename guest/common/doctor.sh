#!/bin/sh
set -eu
set -f

proxy=${HOMEBOX_PROXY_URL:-}
public=${HOMEBOX_PUBLIC_URL:-https://example.com}
failed=0

pass() {
    printf 'PASS %s\n' "$1"
}
fail() {
    printf 'FAIL %s: %s\n' "$1" "$2"
    failed=1
}

if [ -z "$proxy" ]; then
    fail proxy 'HOMEBOX_PROXY_URL is empty'
elif curl -q -fsS --proxy "$proxy" --noproxy '' -m 15 -o /dev/null "$public"; then
    pass proxy
else
    fail proxy 'public request through the proxy failed'
fi

if getent hosts example.com >/dev/null; then
    pass dns
else
    fail dns 'getent could not resolve example.com'
fi

if [ -n "$proxy" ]; then
    if (
        unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY no_proxy NO_PROXY
        curl -q -fsS --noproxy '*' -m 15 -o /dev/null "$public"
    ); then
        pass transparent-tcp
    else
        fail transparent-tcp 'public request without proxy settings failed'
    fi
fi

direct_blocked() {
    name=$1
    url=$2
    status=0
    # shellcheck disable=SC2016
    peer=$(sudo -n -- env HOMEBOX_DOCTOR_URL="$url" sg homebox-direct -c         'exec curl -q -s --noproxy "*" -m 5 -o /dev/null -w "%{remote_ip}" "$HOMEBOX_DOCTOR_URL"') || status=$?
    case "$status:$peer" in
        7:|28:) pass "$name" ;;
        *) fail "$name" "expected a blocked connection (curl exit $status, peer ${peer:-none})" ;;
    esac
}

# Lima grants passwordless sudo as root, which can use sg without a group password.
if [ "$(sudo -n -- sg homebox-direct -c 'id -gn' 2>/dev/null || true)" = homebox-direct ]; then
    direct_blocked direct-public https://1.1.1.1
    for target in ${HOMEBOX_DENY_TARGETS:-}; do
        direct_blocked "direct-$target" "http://$target"
    done
else
    fail direct 'passwordless sudo and sg homebox-direct are required'
fi

if [ -n "$proxy" ]; then
    for target in ${HOMEBOX_DENY_TARGETS:-}; do
        code=$(curl -q -s --proxy "$proxy" --noproxy '' -m 5 -o /dev/null \
            -w '%{http_connect}' "https://$target" || true)
        if [ "$code" = 403 ]; then
            pass "proxy-deny-$target"
        else
            fail "proxy-deny-$target" "expected CONNECT 403, got ${code:-no response}"
        fi
    done
fi

mount=
while read -r _source destination type _rest; do
    case "$type" in
        virtiofs|9p|sshfs|fuse.sshfs) mount=$destination; break ;;
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
