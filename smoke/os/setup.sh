#!/bin/sh
set -eu
set -- inotify-tools
if [ "${SMOKE_LEVEL:-full}" = full ]; then
    set -- "$@" cron
fi
sudo -n env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends "$@"
