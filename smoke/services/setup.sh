#!/bin/sh
set -eu
set -- postgresql redis-server rabbitmq-server nginx caddy mkcert dnsutils python3-venv
if [ "${SMOKE_LEVEL:-full}" = full ]; then
    set -- "$@" mariadb-server valkey-server memcached
fi
sudo -n apt-get update
sudo -n env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends "$@"
python3 -m venv .venv
set -- 'psycopg[binary]==3.2.13' 'redis==6.2.0' 'pika==1.3.2' 'boto3==1.38.46' \
    'websockets==15.0.1' 'grpcio==1.76.0' 'graphql-core==3.2.6'
if [ "${SMOKE_LEVEL:-full}" = full ]; then
    set -- "$@" 'PyMySQL==1.1.1' 'pymemcache==4.0.0' 'pymongo==4.13.2'
fi
.venv/bin/pip --disable-pip-version-check --timeout 20 --retries 2 install "$@"

# shellcheck source=smoke/lib.sh
. ../lib.sh
while read -r image _platform level; do
    case "$image" in ''|\#*) continue ;; esac
    smoke_image_enabled "$level" || continue
    if ! sudo -n docker image inspect "$image" >/dev/null 2>&1; then
        sudo -n docker pull "$image"
    fi
done < images.txt
