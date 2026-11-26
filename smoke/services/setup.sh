#!/bin/sh
set -eu
sudo -n apt-get update
sudo -n env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends \
    postgresql mariadb-server redis-server valkey-server memcached rabbitmq-server \
    nginx caddy mkcert dnsutils python3-venv
python3 -m venv .venv
.venv/bin/pip --disable-pip-version-check --timeout 20 --retries 2 install \
    'psycopg[binary]==3.2.13' 'PyMySQL==1.1.1' 'redis==6.2.0' 'pymemcache==4.0.0' \
    'pika==1.3.2' 'boto3==1.38.46' 'pymongo==4.13.2' 'websockets==15.0.1' \
    'grpcio==1.76.0' 'graphql-core==3.2.6'
sudo -n docker pull dxflrs/garage:v2.6.0
sudo -n docker pull mongo:8.0.10
