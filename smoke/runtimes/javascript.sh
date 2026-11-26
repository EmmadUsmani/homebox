#!/usr/bin/env bash
# shellcheck source=smoke/runtimes/fixture.sh
source ./fixture.sh

node_test() {
    cat > smoke.test.cjs <<'JS'
const assert = require('node:assert/strict')
const test = require('node:test')
test('fixture', () => assert.equal(6 * 7, 42))
JS
    node --test
}

package_managers() {
    export COREPACK_ENABLE_AUTO_PIN=0
    for manager in npm pnpm yarn; do
        mkdir "$manager"
        (
            cd "$manager"
            printf '{"name":"homebox-smoke","private":true}\n' > package.json
            case "$manager" in
                npm) npm install is-number@7.0.0 ;;
                pnpm) npm exec --yes --package=corepack@0.34.5 -- corepack pnpm@10.15.0 add is-number@7.0.0 ;;
                yarn) npm exec --yes --package=corepack@0.34.5 -- corepack yarn@1.22.22 add is-number@7.0.0 ;;
            esac
            node -e "require('node:assert/strict').equal(require('is-number')('42'), true)"
        )
    done
}

native_addon() {
    npm_config_build_from_source=true npm install better-sqlite3@12.4.6
    node <<'JS'
const assert = require('node:assert/strict')
const db = require('better-sqlite3')(':memory:')
assert.equal(db.prepare('SELECT 42 AS value').get().value, 42)
db.close()
JS
}

vite() {
    npm install typescript@5.9.3 vite@7.2.2
    printf '<script type="module" src="/main.ts"></script>\n' > index.html
    printf 'const value: string = "homebox-vite"; document.body.textContent = value;\n' > main.ts
    ./node_modules/.bin/tsc --noEmit --strict main.ts
    ./node_modules/.bin/vite build
    grep -Fq homebox-vite dist/assets/*.js
}

next_app() {
    export NEXT_TELEMETRY_DISABLED=1
    npm install next@16.4.6 react@19.2 react-dom@19.2
    mkdir app
    printf 'export default function Layout({children}) {return <html><body>{children}</body></html>}\n' > app/layout.jsx
    printf 'export default function Page() {return <main>homebox-next</main>}\n' > app/page.jsx
    ./node_modules/.bin/next build
    expect_http / homebox-next ./node_modules/.bin/next start --hostname 127.0.0.1 --port 18765
}

bun_app() {
    printf '{"dependencies":{"is-number":"7.0.0"}}\n' > package.json
    bun install
    cat > smoke.test.ts <<'TS'
import {test, expect} from 'bun:test'
import isNumber from 'is-number'
test('npm dependency', () => expect(isNumber('42')).toBe(true))
TS
    bun test
    printf "Bun.serve({hostname:'127.0.0.1',port:18765,fetch:()=>new Response('homebox-bun')})\n" > server.js
    expect_http / homebox-bun bun server.js
}

deno_app() {
    printf '{"tasks":{"smoke":"deno run main.ts"}}\n' > deno.json
    printf 'import isNumber from "npm:is-number@7.0.0"; if (!isNumber("42")) throw Error("npm import");\n' > main.ts
    printf 'Deno.test("npm import", async () => { await import("./main.ts") });\n' > smoke_test.ts
    deno task smoke
    deno test
}

"$1"
