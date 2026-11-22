#!/bin/sh
set -eu

failed=0
check() {
    label=$1
    shift
    if "$@"; then
        printf 'PASS %s\n' "$label"
    else
        printf 'FAIL %s\n' "$label" >&2
        failed=1
    fi
}

check Docker docker run --rm hello-world
check 'Docker Compose' docker compose version
check 'Docker Buildx' docker buildx version
check mise sh -lc 'mise --version'
check Node sh -lc 'node --version'
check Python sh -lc 'python --version'
check 'Claude Code' sh -lc 'claude --version'
check Codex sh -lc 'codex --version'
check 'sshd refuses agent and X11 forwarding' sh -c \
    'sudo -n sshd -T | grep -qx "allowagentforwarding no" && sudo -n sshd -T | grep -qx "x11forwarding no"'

workspace="$HOME/.local/share/homebox/playwright"
mise="$HOME/.local/bin/mise"
scratch=$(mktemp -d)
server=
trap '[ -z "$server" ] || kill "$server" 2>/dev/null || true; rm -rf "$scratch"' EXIT
printf '<title>homebox self-test</title><h1>homebox self-test</h1>\n' > "$scratch/index.html"
python3 -u -m http.server --bind 127.0.0.1 --directory "$scratch" 0 >"$scratch/server.log" 2>&1 &
server=$!
url=
for _ in 1 2 3 4 5; do
    port=$(sed -n 's/.* port \([0-9][0-9]*\) .*/\1/p' "$scratch/server.log" | head -n 1)
    if [ -n "$port" ]; then
        url="http://127.0.0.1:$port/"
        break
    fi
    sleep 1
done

cat > "$scratch/render.cjs" <<'JS'
const [playwright, url, screenshot, mode] = process.argv.slice(2);
const { chromium } = require(playwright);

async function render() {
    const browser = await chromium.launch({ headless: mode === "headless" });
    try {
        const page = await browser.newPage();
        const response = await page.goto(url);
        if (!response.ok() || await page.title() !== "homebox self-test") {
            throw new Error("Unexpected response from local test page");
        }
        await page.screenshot({ path: screenshot });
    } finally {
        await browser.close();
    }
}

render().catch(error => {
    console.error(error);
    process.exitCode = 1;
});
JS

# Called through check below.
# shellcheck disable=SC2329
render() {
    mode=$1
    shift
    if [ -z "$url" ]; then
        cat "$scratch/server.log" >&2
        return 1
    fi
    "$@" "$mise" exec -- node "$scratch/render.cjs" "$workspace/node_modules/playwright" \
        "$url" "$scratch/$mode.png" "$mode" && test -s "$scratch/$mode.png"
}

check 'Playwright headless screenshot' render headless
check 'Playwright headed screenshot under Xvfb' render headed xvfb-run -a
exit "$failed"
