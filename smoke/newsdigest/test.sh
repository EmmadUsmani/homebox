#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=smoke/lib.sh
. ../lib.sh
area=$PWD
cd source
unit=homebox-smoke-newsdigest
export COMPOSE_PROJECT_NAME=$unit
if [ "${1:-}" = --cleanup ]; then
    if systemctl is-active --quiet "$unit"; then
        sudo -n systemctl stop "$unit"
    fi
    exec timeout 2m docker compose down --volumes
fi

export DATABASE_URL=postgres://postgres@127.0.0.1:5432/news_digest
export REDIS_URL=redis://127.0.0.1:6379
export READER_PUBLISHED_ONLY=1
node_home=$("$HOME/.local/bin/mise" where node@22)
export PATH="$area/tools/node_modules/.bin:$node_home/bin:$PATH"

artifacts="$area/artifacts"
mkdir -p "$artifacts"

run_suite() {
    local log="$artifacts/$1.log"
    shift
    if "$@" >"$log" 2>&1; then
        return
    fi
    grep -m 8 '^not ok' "$log" || true
    tail -n 10 "$log"
    return 1
}

check 'News Digest typecheck' timeout 8m pnpm typecheck
check 'News Digest lint' timeout 8m pnpm lint
check 'News Digest unit tests' run_suite unit timeout 8m pnpm test:unit
check 'News Digest isolated integration tests' run_suite integration timeout 20m pnpm test:integration

boot() {
    timeout 5m docker compose up -d || return
    timeout 60s bash -c \
        'until docker compose exec -T postgres pg_isready -h 127.0.0.1 -U postgres -d news_digest &&
              docker compose exec -T redis redis-cli ping | grep -qx PONG; do sleep 1; done' || return
    timeout 2m pnpm migrate || return
    timeout 30s node "$area/seed.cjs" || return
    sudo -n systemd-run --unit="$unit" --collect --uid="$(id -u)" --gid="$(id -g)" \
        --property=RuntimeMaxSec=10min \
        --working-directory="$PWD/apps/api" \
        --setenv="DATABASE_URL=$DATABASE_URL" --setenv="REDIS_URL=$REDIS_URL" \
        --setenv="READER_PUBLISHED_ONLY=$READER_PUBLISHED_ONLY" \
        "$(command -v node)" dist/main.js || return
    if ! timeout 60s bash -c \
        'until curl -fs --max-time 2 http://127.0.0.1:8000/healthz; do sleep 1; done'; then
        sudo -n journalctl -u "$unit" -n 30 --no-pager
        return 1
    fi
}

check 'Compose Postgres and Redis, app migrations, fixture and API health' boot
check 'Guest Chromium renders persisted edition on home, archive and detail' \
    timeout 90s node "$area/browser.cjs" "$PWD/node_modules/playwright" http://127.0.0.1:8000 "$artifacts"
finish
