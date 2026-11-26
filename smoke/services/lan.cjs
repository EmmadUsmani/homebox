const assert = require("node:assert/strict");
const { execFile, spawn } = require("node:child_process");
const { createHash } = require("node:crypto");
const { createConnection, createServer } = require("node:net");
const { open, stat } = require("node:fs/promises");
const { join } = require("node:path");
const { createInterface } = require("node:readline");
const { promisify } = require("node:util");

const execute = promisify(execFile);
const { SMOKE_VM, SMOKE_SSH_CONFIG, SMOKE_LAN_HOST, SMOKE_ARTIFACTS, SMOKE_PLAYWRIGHT } = process.env;
const options = [
    ...(SMOKE_SSH_CONFIG ? ["-F", SMOKE_SSH_CONFIG] : []),
    "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15",
    "-o", "ServerAliveCountMax=2", "-o", "StrictHostKeyChecking=yes",
    "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ClearAllForwardings=yes",
    "-o", "ControlPath=none",
];
const ssh = (command, timeout = 15000) => execute("ssh", [...options, SMOKE_VM, command], { timeout });
const scp = (from, to) => execute("scp", [...options, "-o", "Compression=no", from, to], { timeout: 120000 });
const host = SMOKE_LAN_HOST.includes(":") ? `[${SMOKE_LAN_HOST}]` : SMOKE_LAN_HOST;
const firstPort = Number(process.env.SMOKE_LAN_PORT_BASE || 12000);
const http = `http://${host}:${firstPort}/`;
const scratch = process.argv[2];
let failed = false;

async function check(label, run) {
    try {
        await run();
        console.log(`PASS ${label}`);
    } catch (error) {
        failed = true;
        console.log(`FAIL ${label}\n    ${error.message}`);
    }
}

async function measureSSH() {
    const connectionTimes = [];
    for (let sample = 0; sample < 5; sample++) {
        const start = performance.now();
        await ssh("true");
        connectionTimes.push(performance.now() - start);
    }
    console.log(`SSH fresh connection + true: median ${median(connectionTimes)} ms (5 samples)`);

    const child = spawn("ssh", [...options, SMOKE_VM, "cat"], { stdio: ["pipe", "pipe", "inherit"] });
    child.on("error", error => child.stdout.destroy(error));
    child.stdin.on("error", error => child.stdout.destroy(error));
    const reader = createInterface({ input: child.stdout });
    const lines = reader[Symbol.asyncIterator]();
    const timer = setTimeout(() => child.kill(), 10000);
    const roundTrips = [];
    try {
        // The first echo opens the channel and warms it up.
        for (let sample = 0; sample <= 5; sample++) {
            const line = `sample-${sample}`;
            const start = performance.now();
            child.stdin.write(line + "\n");
            assert.equal((await lines.next()).value, line);
            if (sample > 0) roundTrips.push(performance.now() - start);
        }
    } finally {
        clearTimeout(timer);
        reader.close();
        child.stdin.end();
    }
    console.log(`SSH established channel echo: median ${median(roundTrips)} ms (5 samples)`);
}

function median(samples) {
    return samples.sort((a, b) => a - b)[Math.floor(samples.length / 2)].toFixed(1);
}

async function measureSCP() {
    const bytes = 200_000_000;
    const upload = join(scratch, "upload");
    const download = join(scratch, "download");
    const file = await open(upload, "w");
    try {
        await file.truncate(bytes);
    } finally {
        await file.close();
    }
    const { stdout } = await ssh("mktemp -d /tmp/homebox-smoke-scp.XXXXXXXX");
    const dir = stdout.trim();
    assert.match(dir, /^\/tmp\/homebox-smoke-scp\.[A-Za-z0-9]+$/);
    try {
        const start = performance.now();
        await scp(upload, `${SMOKE_VM}:${dir}/sample`);
        const uploaded = performance.now();
        await scp(`${SMOKE_VM}:${dir}/sample`, download);
        const downloaded = performance.now();
        assert.equal((await stat(download)).size, bytes);
        console.log(`SCP 200 MB: upload ${(200000 / (uploaded - start)).toFixed(1)} MB/s, download ${(200000 / (downloaded - uploaded)).toFixed(1)} MB/s`);
    } finally {
        await ssh(`rm -f '${dir}/sample'; rmdir '${dir}'`);
    }
}

async function verifyEgress() {
    // Derive the laptop's source address from its route to the VM's SSH port.
    // SSH_CONNECTION inside the VM only sees homebox's forwarding path.
    const { stdout } = await execute("ssh", [...options, "-G", SMOKE_VM]);
    const port = Number(stdout.match(/^port (\d+)$/m)[1]);
    const address = await new Promise((resolve, reject) => {
        const socket = createConnection({ host: SMOKE_LAN_HOST, port, family: 4 });
        socket.setTimeout(5000, () => socket.destroy(new Error("LAN route timed out")));
        socket.once("error", reject);
        socket.once("connect", () => {
            resolve(socket.localAddress);
            socket.destroy();
        });
    });
    assert.match(address, /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)[\d.]+$/);
    let guestReachedLaptop = false;
    const listener = createServer(socket => {
        guestReachedLaptop = true;
        socket.destroy();
    });
    try {
        await new Promise((resolve, reject) => {
            listener.once("error", reject);
            listener.listen(0, address, resolve);
        });
        const { port } = listener.address();
        const result = await ssh(`command -v timeout >/dev/null && command -v bash >/dev/null || exit 2
            timeout 4 bash -c 'exec 3<>/dev/tcp/${address}/${port}' >/dev/null 2>&1
            status=$?
            [ "$status" -eq 1 ] || [ "$status" -eq 124 ]`);
        assert.equal(result.stdout, "");
        assert.equal(guestReachedLaptop, false);
    } finally {
        listener.close();
    }
}

async function verifyPrivateRanges() {
    // Match the probe destinations used by the container smoke tests.
    await ssh(`set -eu
        command -v timeout >/dev/null
        command -v bash >/dev/null
        for target in 10.255.255.254 172.31.255.254 192.168.255.254; do
            status=0
            timeout 2 bash -c 'exec 3<>/dev/tcp/"$1"/80' smoke "$target" >/dev/null 2>&1 || status=$?
            [ "$status" -eq 1 ] || [ "$status" -eq 124 ] || exit 1
        done`);
}

async function verifyWebSocket() {
    await new Promise((resolve, reject) => {
        const socket = new WebSocket(`ws://${host}:${firstPort + 1}/`);
        const timer = setTimeout(() => {
            socket.close();
            reject(new Error("WebSocket echo timed out"));
        }, 5000);
        socket.addEventListener("open", () => socket.send("homebox smoke"), { once: true });
        const finish = (error, message) => {
            clearTimeout(timer);
            socket.close();
            if (error) reject(error);
            else resolve(message);
        };
        socket.addEventListener("error", () => finish(new Error("WebSocket connection failed")), { once: true });
        socket.addEventListener("message", event => finish(null, event.data), { once: true });
    }).then(message => assert.equal(message, "homebox smoke"));
}

async function verifyBrowser() {
    const { chromium } = require(SMOKE_PLAYWRIGHT);
    const browser = await chromium.launch();
    try {
        const page = await browser.newPage();
        const response = await page.goto(http, { timeout: 10000 });
        assert.equal(response.status(), 200);
        assert.equal(await page.locator("body").innerText({ timeout: 5000 }), "homebox-smoke");
        const payload = "homebox smoke from laptop Chromium";
        const digest = await page.evaluate(async body => {
            const result = await fetch("/", { method: "POST", body, signal: AbortSignal.timeout(5000) });
            if (!result.ok) throw new Error(`HTTP ${result.status}`);
            return result.text();
        }, payload);
        assert.equal(digest, createHash("sha256").update(payload).digest("hex"));
        await page.screenshot({ path: join(SMOKE_ARTIFACTS, "services-laptop-chromium.png"), timeout: 5000 });
    } finally {
        await browser.close();
    }
}

async function main() {
    assert(Number.isInteger(firstPort) && firstPort >= 1 && firstPort < 65535, "Invalid SMOKE_LAN_PORT_BASE");
    const unit = `homebox-smoke-lan-${process.pid}`;
    try {
        await ssh(`set -eu
            cd ~/homebox-smoke/services
            systemd-run --user --unit=${unit}-http --property=RuntimeMaxSec=120 --collect "$PWD/.venv/bin/python" "$PWD/web.py" http 0.0.0.0 8000
            systemd-run --user --unit=${unit}-ws --property=RuntimeMaxSec=120 --collect "$PWD/.venv/bin/python" "$PWD/web.py" ws 0.0.0.0 8001`);
        await check("Laptop reaches guest HTTP on a published LAN port", async () => {
            const { stdout } = await execute("curl", ["--noproxy", "*", "--retry", "5", "--retry-all-errors",
                "--retry-max-time", "12", "--max-time", "3", "-fsS", http]);
            assert.equal(stdout, "homebox-smoke");
        });
        await check("Laptop exchanges a WebSocket message on a published LAN port", verifyWebSocket);
        if (SMOKE_PLAYWRIGHT) {
            await check("Laptop Chromium renders the guest HTTP fixture", verifyBrowser);
        }
    } finally {
        await ssh(`systemctl --user stop ${unit}-http ${unit}-ws`).catch(() => {});
    }
    await check("Guest cannot reach a live listener on the laptop's private LAN address", verifyEgress);
    await check("Guest cannot connect to the private-range test destinations", verifyPrivateRanges);
    await check("Pinned SSH connects and echoes data on the established channel", measureSSH);
    await check("SCP uploads and downloads a 200 MB file", measureSCP);
}

main().catch(error => {
    console.log(`FAIL Laptop LAN checks: ${error.message}`);
    failed = true;
}).finally(() => {
    process.exitCode = failed ? 1 : 0;
});
