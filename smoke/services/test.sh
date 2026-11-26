#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=../lib.sh
. ../lib.sh

SMOKE_TMP=$(mktemp -d)
export SMOKE_TMP
python=$PWD/.venv/bin/python
pids=()
cleanup() {
    for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
    wait || true
    sudo -n docker rm -fv homebox-smoke-garage homebox-smoke-mongo >/dev/null 2>&1 || true
    rm -rf "$SMOKE_TMP"
}
trap cleanup EXIT

wait_for() {
    local _try
    for _try in {1..30}; do
        if "$@" >/dev/null 2>&1; then return 0; fi
        sleep 1
    done
    "$@"
}

probe() {
    timeout 25 "$python" stores.py "$@"
}

sqlite_test() {
    probe sqlite write && probe sqlite read
}

postgres_test() (
    set -e
    role=$(id -un)
    created_role=false
    created_database=false
    # shellcheck disable=SC2329 # Invoked by the EXIT trap.
    cleanup_postgres() {
        if "$created_database"; then sudo -n -u postgres dropdb homebox_smoke; fi
        if "$created_role"; then sudo -n -u postgres dropuser "$role"; fi
    }
    trap cleanup_postgres EXIT
    if ! sudo -n -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname = '$role'" | grep -qx 1; then
        sudo -n -u postgres createuser "$role"
        created_role=true
    fi
    sudo -n -u postgres createdb -O "$role" homebox_smoke
    created_database=true
    probe postgres write
    sudo -n systemctl restart postgresql
    probe postgres read
)

mariadb_test() (
    set -e
    role=$(id -un)
    created_role=false
    created_database=false
    # shellcheck disable=SC2329 # Invoked by the EXIT trap.
    cleanup_mariadb() {
        if "$created_database"; then sudo -n mariadb -e "DROP DATABASE homebox_smoke;"; fi
        if "$created_role"; then sudo -n mariadb -e "DROP USER '$role'@localhost;"; fi
    }
    trap cleanup_mariadb EXIT
    if ! sudo -n mariadb -Nse "SELECT 1 FROM mysql.user WHERE User = '$role' AND Host = 'localhost'" | grep -qx 1; then
        sudo -n mariadb -e "CREATE USER '$role'@localhost IDENTIFIED VIA unix_socket;"
        created_role=true
    fi
    sudo -n mariadb -e "CREATE DATABASE homebox_smoke;"
    created_database=true
    sudo -n mariadb -e "GRANT ALL ON homebox_smoke.* TO '$role'@localhost;"
    probe mariadb write
    sudo -n systemctl restart mariadb
    probe mariadb read
)

redis_test() (
    set -e
    name=$1
    port=$2
    mkdir "$SMOKE_TMP/$name"
    start() {
        "$name-server" --bind 127.0.0.1 --port "$port" --dir "$SMOKE_TMP/$name" &
        server=$!
        wait_for "$name-cli" -p "$port" ping
    }
    trap 'kill "${server:-}" 2>/dev/null || true; wait || true' EXIT
    start
    probe "$name" write
    kill "$server"
    wait "$server"
    start
    probe "$name" read
)

unit_store() (
    set -e
    probe "$1" write
    sudo -n systemctl restart "$2"
    wait_for probe "$1" read
)

container_store() (
    set -e
    name=$1
    container=$2
    shift 2
    sudo -n docker run -d --name "$container" "$@"
    # Allow the server's first-run storage initialization to finish.
    case "$name" in
        s3) wait_for sudo -n docker exec "$container" /garage bucket info homebox-smoke ;;
        mongo) wait_for sudo -n docker exec "$container" mongosh --quiet --eval 'db.adminCommand({ping: 1}).ok' ;;
    esac
    probe "$name" write
    sudo -n docker restart "$container"
    wait_for probe "$name" read
)

garage_test() (
    set -e
    umask 077
    cat > "$SMOKE_TMP/garage.toml" <<CONFIG
metadata_dir = "/meta"
data_dir = "/data"
db_engine = "sqlite"
replication_factor = 1
rpc_bind_addr = "127.0.0.1:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "$(openssl rand -hex 32)"

[s3_api]
s3_region = "garage"
api_bind_addr = "0.0.0.0:3900"
CONFIG
    GARAGE_DEFAULT_ACCESS_KEY="GK$(openssl rand -hex 12)"
    GARAGE_DEFAULT_SECRET_KEY=$(openssl rand -hex 32)
    export GARAGE_DEFAULT_ACCESS_KEY GARAGE_DEFAULT_SECRET_KEY
    env_file="$SMOKE_TMP/garage.env"
    printf '%s\n' "GARAGE_DEFAULT_ACCESS_KEY=$GARAGE_DEFAULT_ACCESS_KEY" \
        "GARAGE_DEFAULT_SECRET_KEY=$GARAGE_DEFAULT_SECRET_KEY" \
        GARAGE_DEFAULT_BUCKET=homebox-smoke >"$env_file"
    container_store s3 homebox-smoke-garage \
        -p 127.0.0.1:19000:3900 -v /meta -v /data \
        -v "$SMOKE_TMP/garage.toml:/etc/garage.toml:ro" --env-file "$env_file" \
        dxflrs/garage:v2.6.0 /garage server --single-node --default-bucket
)

export python
export -f wait_for probe sqlite_test postgres_test mariadb_test redis_test unit_store container_store garage_test

store() {
    check "$1" timeout 120 bash -euo pipefail -c '"$@"' smoke "${@:2}"
}

store 'SQLite WAL, FTS5 and JSON survive reopening the database' sqlite_test
store 'PostgreSQL extensions and rows survive service restart' postgres_test
store 'MariaDB rows survive service restart' mariadb_test
store 'Redis pub/sub, streams and snapshot reload' redis_test redis 16379
store 'Valkey pub/sub, streams and snapshot reload' redis_test valkey 16380
store 'Memcached round trip and expected cache loss on restart' unit_store memcached memcached
store 'RabbitMQ durable queue delivers a confirmed message after restart' unit_store rabbitmq rabbitmq-server
store 'Garage S3 object survives container restart' garage_test
store 'MongoDB ARM64 document survives container restart' container_store mongo homebox-smoke-mongo \
    -p 127.0.0.1:19001:27017 mongo:8.0.10

"$python" web.py http >"$SMOKE_TMP/http.log" 2>&1 &
pids+=("$!")
"$python" web.py ws >"$SMOKE_TMP/ws.log" 2>&1 &
pids+=("$!")
cat > "$SMOKE_TMP/nginx.conf" <<'NGINX'
pid nginx.pid;
error_log stderr;
events {}
http {
  access_log off;

  client_body_temp_path body;
  proxy_temp_path proxy;
  fastcgi_temp_path fastcgi;
  uwsgi_temp_path uwsgi;
  scgi_temp_path scgi;
  server {
    listen 127.0.0.1:18102;
    client_max_body_size 20m;
    location / { proxy_pass http://127.0.0.1:18100; proxy_buffering off; }
  }
}
NGINX
nginx -p "$SMOKE_TMP/" -c nginx.conf -g 'daemon off;' >"$SMOKE_TMP/nginx.log" 2>&1 &
pids+=("$!")
export CAROOT=$SMOKE_TMP/ca
mkdir "$CAROOT"
mkcert -cert-file "$SMOKE_TMP/cert.pem" -key-file "$SMOKE_TMP/key.pem" localhost
cat > "$SMOKE_TMP/Caddyfile" <<CADDY
{
    admin off
    auto_https disable_redirects
}
https://localhost:18103 {
    bind 127.0.0.1
    tls $SMOKE_TMP/cert.pem $SMOKE_TMP/key.pem
    reverse_proxy 127.0.0.1:18100
}
http://127.0.0.1:18104 {
    reverse_proxy 127.0.0.1:18101
}
CADDY
caddy run --adapter caddyfile --config "$SMOKE_TMP/Caddyfile" >"$SMOKE_TMP/caddy.log" 2>&1 &
pids+=("$!")

http_test() {
    wait_for curl -fsS --max-time 2 http://127.0.0.1:18102/ &&
        [[ $(curl -fsS --max-time 5 --http1.1 -w ' %{http_version}' http://127.0.0.1:18102/) == 'homebox-smoke 1.1' ]]
}
tls_test() {
    local -a tls=(curl -fsS --max-time 5 --cacert "$CAROOT/rootCA.pem" --http2)
    wait_for "${tls[@]}" https://localhost:18103/ &&
        [[ $("${tls[@]}" -w ' %{http_version}' https://localhost:18103/) == 'homebox-smoke 2' ]]
}
transfer() (
    set -e
    local url=http://127.0.0.1:18102
    head -c 16777216 /dev/zero >"$SMOKE_TMP/blob"
    digest=$(sha256sum "$SMOKE_TMP/blob" | cut -d ' ' -f 1)
    [[ $(curl -fsS --max-time 15 --data-binary @"$SMOKE_TMP/blob" "$url/upload") == "$digest" ]]
    curl -fsS --max-time 15 "$url/blob" >"$SMOKE_TMP/download"
    cmp "$SMOKE_TMP/download" <(tr '\000' x <"$SMOKE_TMP/blob")
)
graphql() {
    curl -fsS --max-time 5 -H 'Content-Type: application/json' \
        --data '{"query":"{ echo(value: \"smoke\") }"}' http://127.0.0.1:18102/graphql |
        jq -e '.data.echo == "smoke"'
}
sse() {
    curl -fsSN --max-time 8 http://127.0.0.1:18102/events |
        diff - <(printf 'data: first\n\ndata: second\n\n')
}

check 'nginx proxies an HTTP/1.1 request' http_test
check 'Caddy proxies HTTP/2 with a verified local CA' tls_test
check 'Caddy proxies WebSocket messages on an open connection' timeout 25 "$python" web.py ws-client ws://127.0.0.1:18104/
check 'nginx proxies two server-sent events on one connection' sse
export -f transfer
check 'nginx proxies 16 MiB uploads and downloads intact' bash -euo pipefail -c transfer
check 'GraphQL resolves a query over HTTP' graphql
check 'gRPC echoes a payload over HTTP/2' timeout 15 "$python" web.py grpc
check 'Chromium WebRTC peers exchange data and receive a video frame' timeout 40 node rtc.cjs local
check 'Chromium gathers a public STUN server-reflexive candidate' timeout 30 node rtc.cjs stun

git_clone() {
    local attempt
    for attempt in 1 2 3; do
        if timeout 20 git clone -4 --depth=1 https://github.com/octocat/Hello-World.git "$SMOKE_TMP/git-$attempt"; then
            return 0
        fi
        sleep "$attempt"
    done
    return 1
}

check 'Git shallow clone over HTTPS' git_clone
for url in https://registry.npmjs.org/ https://pypi.org/simple/pytest/ https://crates.io/api/v1/crates/serde https://github.com/; do
    check "HTTPS $url" curl -4fsS --connect-timeout 6 --max-time 15 \
        --retry 2 --retry-all-errors --retry-max-time 35 -A homebox-smoke -o /dev/null "$url"
done

dns() {
    dig +time=3 +tries=2 +short "$2" "$1" | grep -E "."
}

for pair in 'A example.com' 'AAAA example.com' 'TXT example.com' 'MX gmail.com'; do
    read -r type domain <<< "$pair"
    check "DNS $type $domain" dns "$type" "$domain"
done
check_sh 'IPv4 HTTPS works while IPv6 has no public route' \
    'curl -4fsS --max-time 8 -o /dev/null https://www.cloudflare.com/ &&
     ! curl -6fsS --max-time 5 -o /dev/null https://www.cloudflare.com/'
finish
