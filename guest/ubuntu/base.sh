#!/bin/sh
set -eu

usermod -aG systemd-journal "$HOMEBOX_USER"

export DEBIAN_FRONTEND=noninteractive
apt-get update
# Development and diagnostic tools drawn from the exeuntu package list.
apt-get install -y --no-install-recommends \
    build-essential pkg-config ca-certificates curl wget git jq sqlite3 \
    ripgrep fd-find tmux unzip zip xz-utils zstd file less tree vim neovim \
    python3 python3-pip python3-venv pipx locales tzdata \
    openssh-server openssh-client sudo lsof iproute2 iputils-ping dnsutils \
    netcat-openbsd socat rsync psmisc btop ncdu man-db manpages-dev \
    bubblewrap dbus-user-session imagemagick ffmpeg

locale-gen en_US.UTF-8
update-locale LANG=en_US.UTF-8
timedatectl set-timezone Etc/UTC

# The VM must never reach the laptop's SSH agent or display, whatever the
# client asks for.
cat > /etc/ssh/sshd_config.d/10-homebox.conf <<'EOF'
AllowAgentForwarding no
X11Forwarding no
EOF
sshd -t
systemctl enable ssh.service
systemctl reload-or-restart ssh.service
