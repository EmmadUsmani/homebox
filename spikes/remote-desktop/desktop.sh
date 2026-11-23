#!/bin/sh
# Spike only: a minimal Xfce desktop plus the candidate display servers.
# Not part of any guest profile. Run as root in a scratch VM.
set -eu
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y --no-install-recommends \
    xfce4-session xfwm4 xfce4-panel xfdesktop4 xfce4-settings xfce4-terminal \
    xfconf thunar dbus-x11 x11-xserver-utils xdotool mpv pv ffmpeg \
    fonts-dejavu-core \
    tigervnc-standalone-server tigervnc-tools novnc python3-websockify

# KasmVNC is not in Ubuntu's archive. Pin the release asset and its digest.
kasm_url=https://github.com/kasmtech/KasmVNC/releases/download/v1.5.2/kasmvncserver_ubuntu_resolute_1.5.2_arm64.deb
kasm_sha256=634f1e992ff625cb1245de206c7af96be67bea2aade89dfa5853f1c0a47c5fd1
deb=$(mktemp --suffix=.deb)
trap 'rm -f "$deb"' EXIT
curl -fsSL -o "$deb" "$kasm_url"
printf '%s  %s\n' "$kasm_sha256" "$deb" | sha256sum -c -
apt-get install -y "$deb"
# KasmVNC reads its certificate group membership at login.
usermod -aG ssl-cert "$HOMEBOX_USER"
