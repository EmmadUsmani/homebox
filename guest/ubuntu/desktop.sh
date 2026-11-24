#!/bin/sh
# Opt-in layer, installed into an existing Ubuntu guest over pinned SSH.
set -eu
export DEBIAN_FRONTEND=noninteractive
dir=/usr/local/lib/homebox-desktop
install -d -m 755 "$dir"
exec 9>"$dir/install.lock"
flock -w 120 9
chmod 644 "$dir/install.lock"

version=1.5.2
arch=$(dpkg --print-architecture)
case "$arch" in
    arm64) digest=634f1e992ff625cb1245de206c7af96be67bea2aade89dfa5853f1c0a47c5fd1 ;;
    amd64) digest=caaae8b563570cabed436914187b961f7b563f9e4473e3bb3b09d99e0fd3348d ;;
    *) echo "Desktop layer is unavailable for $arch" >&2; exit 1 ;;
esac

if [ ! -f "$dir/kasmvnc-$digest" ] ||
    [ "$(dpkg-query -W -f='${Status} ${Version}' kasmvncserver 2>/dev/null || true)" != "install ok installed $version-1" ]; then
    deb=$(mktemp --suffix=.deb)
    trap 'rm -f "$deb"' EXIT
    url="https://github.com/kasmtech/KasmVNC/releases/download/v$version/kasmvncserver_ubuntu_resolute_${version}_$arch.deb"
    curl -fsSL --connect-timeout 10 --max-time 120 -o "$deb" "$url"
    printf '%s  %s\n' "$digest" "$deb" | sha256sum -c -
    chmod 644 "$deb"
    apt-get -o Acquire::Retries=2 -o Acquire::http::Timeout=30 update
    apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends \
        xfce4-session xfwm4 xfce4-panel xfdesktop4 xfce4-settings xfce4-terminal \
        xfconf thunar dbus-x11 xauth x11-utils x11-xserver-utils fonts-dejavu-core \
        libseccomp2 python3 "$deb"
    touch "$dir/kasmvnc-$digest"
fi

# KasmVNC 1.5.2 starts its UDP transport even when the client uses WebSockets.
# Refuse IPv4/IPv6 datagram sockets in Xkasmvnc, before it opens any listeners.
cat > "$dir/tcp-only.py" <<'PY'
import ctypes
import errno
import os
import socket
import sys

class Comparison(ctypes.Structure):
    _fields_ = [
        ("arg", ctypes.c_uint),
        ("op", ctypes.c_int),
        ("mask", ctypes.c_uint64),
        ("value", ctypes.c_uint64),
    ]

lib = ctypes.CDLL("libseccomp.so.2")
lib.seccomp_init.argtypes = [ctypes.c_uint32]
lib.seccomp_init.restype = ctypes.c_void_p
lib.seccomp_syscall_resolve_name.argtypes = [ctypes.c_char_p]
lib.seccomp_rule_add_array.argtypes = [
    ctypes.c_void_p, ctypes.c_uint32, ctypes.c_int, ctypes.c_uint,
    ctypes.POINTER(Comparison),
]
lib.seccomp_load.argtypes = [ctypes.c_void_p]
lib.seccomp_release.argtypes = [ctypes.c_void_p]

allow = 0x7fff0000
deny = 0x00050000 | errno.EACCES
equal = 4
masked_equal = 7
socket_type_mask = 0xf  # Exclude SOCK_CLOEXEC and SOCK_NONBLOCK.
rules = lib.seccomp_init(allow)
socket_syscall = lib.seccomp_syscall_resolve_name(b"socket")
if not rules or socket_syscall < 0:
    sys.exit("Cannot initialize KasmVNC socket filter")
for family in (socket.AF_INET, socket.AF_INET6):
    comparisons = (Comparison * 2)(
        Comparison(0, equal, family, 0),
        Comparison(1, masked_equal, socket_type_mask, socket.SOCK_DGRAM),
    )
    if lib.seccomp_rule_add_array(rules, deny, socket_syscall, 2, comparisons) != 0:
        sys.exit("Cannot configure KasmVNC socket filter")
if lib.seccomp_load(rules) != 0:
    sys.exit("Cannot load KasmVNC socket filter")
lib.seccomp_release(rules)
os.execv(sys.argv[1], sys.argv[1:])
PY

cat > "$dir/session" <<'SESSION'
#!/bin/sh
set -eu
umask 077
export DISPLAY=:2
export XAUTHORITY="$RUNTIME_DIRECTORY/Xauthority"
touch "$XAUTHORITY"
xauth add "$DISPLAY" . "$(mcookie)"
unset SESSION_MANAGER DBUS_SESSION_BUS_ADDRESS

/usr/bin/python3 /usr/local/lib/homebox-desktop/tcp-only.py /usr/bin/Xkasmvnc "$DISPLAY" \
    -auth "$XAUTHORITY" -nolisten tcp -geometry 1600x900 -depth 24 \
    -interface 127.0.0.1 -UseIPv6=0 -websocketPort 8444 -sslOnly=0 \
    -httpd /usr/share/kasmvnc/www -disableBasicAuth -SecurityTypes None -publicIP 127.0.0.1 \
    -http-header Cross-Origin-Embedder-Policy=require-corp \
    -http-header Cross-Origin-Opener-Policy=same-origin \
    -IgnoreClientSettingsKasm=1 -FrameRate 60 -AcceptSetDesktopSize=0 \
    -SendCutText=0 -AcceptCutText=0 -SendPrimary=0 -SetPrimary=0 \
    -AllowOverride= -AlwaysShared=1 -DisconnectClients=0 &
display_pid=$!
for attempt in $(seq 1 40); do
    if ! kill -0 "$display_pid" 2>/dev/null; then
        echo 'KasmVNC exited before Xfce could start' >&2
        exit 1
    fi
    if xdpyinfo >/dev/null 2>&1; then
        exec dbus-run-session -- startxfce4
    fi
    sleep 0.25
done
echo 'Timed out waiting for KasmVNC X display' >&2
exit 1
SESSION

cat > "$dir/ready" <<'READY'
#!/bin/sh
set -eu
export DISPLAY=:2
export XAUTHORITY="$RUNTIME_DIRECTORY/Xauthority"
for attempt in $(seq 1 40); do
    if xprop -root _NET_SUPPORTING_WM_CHECK 2>/dev/null | grep -q 'window id #' &&
        curl --noproxy '*' -fsS --max-time 1 -o /dev/null http://127.0.0.1:8444/; then
        exit 0
    fi
    sleep 0.25
done
echo 'Timed out waiting for Xfce and KasmVNC HTTP' >&2
exit 1
READY

cat > /etc/systemd/system/homebox-desktop@.service <<'UNIT'
[Unit]
Description=homebox Xfce desktop for %I

[Service]
User=%I
RuntimeDirectory=homebox-desktop-%i
RuntimeDirectoryMode=0700
ExecStart=/usr/local/lib/homebox-desktop/session
ExecStartPost=/usr/local/lib/homebox-desktop/ready
TimeoutStartSec=30
TimeoutStopSec=10
UNIT

cat > "$dir/start" <<'START'
#!/bin/sh
set -eu
exec 9</usr/local/lib/homebox-desktop/install.lock
if ! flock -s -w 5 9; then
    echo 'Desktop installation is in progress. Try again when it finishes.' >&2
    exit 1
fi
unit=$(systemd-escape --template=homebox-desktop@.service "$(id -un)")
if ! sudo -n systemctl start "$unit"; then
    echo "Desktop startup failed. Inspect: sudo journalctl -u $unit" >&2
    exit 1
fi
START
chmod 755 "$dir/session" "$dir/ready" "$dir/start"
systemctl daemon-reload
