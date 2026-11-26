# shellcheck shell=sh
# Source on the guest to target the desktop created by box desktop.
export DISPLAY=:2
XAUTHORITY="/run/homebox-desktop-$(systemd-escape "$(id -un)")/Xauthority"
export XAUTHORITY
