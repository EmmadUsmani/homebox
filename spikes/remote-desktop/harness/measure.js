// Measures a browser remote-desktop client through a counting TCP proxy.
//
//   node measure.js --name NAME --target PORT --path /vnc.html?... \
//                   --guest "box ssh desk --" --out results.json
//
// PORT is the Mac-side end of an SSH local forward to the guest's loopback
// web client. The proxy counts every byte the client and server exchange.
"use strict";

const net = require("node:net");
const fs = require("node:fs");
const { execFileSync } = require("node:child_process");
const { chromium } = require("playwright");

const args = Object.fromEntries(process.argv.slice(2).reduce((pairs, value, index, all) =>
    index % 2 === 0 ? [...pairs, [value.replace(/^--/, ""), all[index + 1]]] : pairs, []));
const guest = args.guest.split(" ");
const trials = Number(args.trials ?? 60);
const sceneSeconds = Number(args.seconds ?? 20);

let down = 0;
let up = 0;
function proxy(target) {
    return new Promise((resolve) => {
        const server = net.createServer((client) => {
            const upstream = net.connect(target, "127.0.0.1");
            client.on("data", (chunk) => { up += chunk.length; upstream.write(chunk); });
            upstream.on("data", (chunk) => { down += chunk.length; client.write(chunk); });
            const end = () => { client.destroy(); upstream.destroy(); };
            client.on("close", end); upstream.on("close", end);
            client.on("error", end); upstream.on("error", end);
        });
        server.listen(0, "127.0.0.1", () => resolve(server));
    });
}

function inGuest(script) {
    return execFileSync(guest[0], [...guest.slice(1), script], { encoding: "utf8" });
}

// Total guest CPU busy percentage over a window, from /proc/stat.
function cpuSample() {
    const [user, nice, system, idle, iowait, irq, softirq, steal] =
        inGuest("head -1 /proc/stat").trim().split(/\s+/).slice(1).map(Number);
    return { busy: user + nice + system + irq + softirq + steal, total: user + nice + system + idle + iowait + irq + softirq + steal };
}

async function scene(page, label, start) {
    if (start) inGuest(start);
    await page.waitForTimeout(3000);
    const before = { down, up, cpu: cpuSample(), at: Date.now() };
    await page.waitForTimeout(sceneSeconds * 1000);
    const cpu = cpuSample();
    const seconds = (Date.now() - before.at) / 1000;
    const result = {
        scene: label,
        downKBps: +((down - before.down) / 1024 / seconds).toFixed(1),
        upKBps: +((up - before.up) / 1024 / seconds).toFixed(1),
        guestCPUPercent: +(100 * (cpu.busy - before.cpu.busy) / (cpu.total - before.cpu.total)).toFixed(1),
    };
    console.log(JSON.stringify(result));
    return result;
}

async function main() {
    const server = await proxy(Number(args.target));
    const port = server.address().port;
    const browser = await chromium.launch();
    const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
    // Optional client settings, e.g. --storage '{"framerate":"60"}' for KasmVNC.
    if (args.storage) {
        await page.addInitScript((settings) => {
            for (const [key, value] of Object.entries(settings)) localStorage.setItem(key, value);
        }, JSON.parse(args.storage));
    }
    await page.goto(`http://127.0.0.1:${port}${args.path}`);
    await page.waitForFunction(() => [...document.querySelectorAll("canvas")]
        .some((canvas) => canvas.width > 800 && canvas.height > 500), null, { timeout: 30000 });
    await page.waitForTimeout(4000);
    await page.evaluate(() => {
        const sample = document.createElement("canvas");
        sample.width = 400; sample.height = 60;
        const ctx = sample.getContext("2d", { willReadFrequently: true });
        window.__screen = () => [...document.querySelectorAll("canvas")]
            .sort((a, b) => b.width * b.height - a.width * a.height)[0];
        // Hash the strip under the panel where the maximised terminal's
        // prompt and typed text appear, away from the panel clock.
        window.__hash = () => {
            const screen = window.__screen();
            ctx.drawImage(screen, 0, screen.height * 0.08, screen.width * 0.5, screen.height * 0.12, 0, 0, 400, 60);
            const data = ctx.getImageData(0, 0, 400, 60).data;
            let h = 0;
            for (let i = 0; i < data.length; i += 4) h = (h * 31 + data[i] + data[i + 1] * 7 + data[i + 2] * 13) | 0;
            return h;
        };
    });
    await page.screenshot({ path: `${args.name}-desktop.png` });

    // A maximised terminal in the middle of the screen receives the keys.
    inGuest("DISPLAY=:" + args.display + " xdotool search --class xfce4-terminal windowactivate --sync >/dev/null 2>&1 || true");
    const box = await page.evaluate(() => { const r = window.__screen().getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 }; });
    await page.mouse.click(box.x, box.y);
    await page.waitForTimeout(500);

    // Latency via Chrome's screencast, content-aware: the compositor streams
    // frames stamped with the browser's clock; each frame is decoded and the
    // strip where typed text appears is compared with the frame before the
    // key press. Works for clients that draw from a worker or stream video
    // continuously, and adds the same overhead to every candidate.
    if (args.method === "screencast") {
        const jpeg = require("jpeg-js");
        const cdp = await page.context().newCDPSession(page);
        const strip = (data) => {
            const image = jpeg.decode(Buffer.from(data, "base64"), { useTArray: true });
            const scale = image.width / 1600;
            const x0 = 0, x1 = Math.round(600 * scale), y0 = Math.round(120 * scale), y1 = Math.round(160 * scale);
            const out = [];
            for (let y = y0; y < y1; y++) for (let x = x0; x < x1; x++) {
                const i = (y * image.width + x) * 4;
                out.push(image.data[i] + image.data[i + 1] + image.data[i + 2]);
            }
            return out;
        };
        const differs = (a, b) => { let sum = 0; for (let i = 0; i < a.length; i++) sum += Math.abs(a[i] - b[i]); return sum / a.length > 4; };
        let last = null;
        let watch = null;
        cdp.on("Page.screencastFrame", (frame) => {
            cdp.send("Page.screencastFrameAck", { sessionId: frame.sessionId }).catch(() => {});
            const pixels = strip(frame.data);
            const at = frame.metadata.timestamp * 1000;
            if (watch && at >= watch.sent && differs(pixels, watch.base)) { watch.resolve(at - watch.sent); watch = null; }
            last = pixels;
        });
        await cdp.send("Page.startScreencast", { format: "jpeg", quality: 80, maxWidth: 1600, maxHeight: 1000, everyNthFrame: 1 });
        await page.waitForTimeout(1000);
        const latencies = [];
        for (let i = 0; i < trials; i++) {
            await page.waitForTimeout(400);
            const changed = new Promise((resolve) => {
                watch = { base: last, sent: Date.now(), resolve };
                setTimeout(() => { if (watch) { watch = null; resolve(-1); } }, 2000);
            });
            await page.keyboard.press(i % 2 === 0 ? "x" : "Backspace");
            latencies.push(await changed);
        }
        await cdp.send("Page.stopScreencast");
        return finish(latencies);
    }
    const latencies = [];
    for (let i = 0; i < trials; i++) {
        const key = i % 2 === 0 ? "x" : "Backspace";
        await page.evaluate(() => {
            window.__h0 = window.__hash();
            window.__t0 = performance.now();
            window.__changed = new Promise((resolve) => {
                const tick = () => window.__hash() !== window.__h0 ? resolve(performance.now() - window.__t0) : requestAnimationFrame(tick);
                requestAnimationFrame(tick);
                setTimeout(() => resolve(-1), 2000);
            });
        });
        await page.keyboard.press(key);
        latencies.push(await page.evaluate(() => window.__changed));
        await page.waitForTimeout(150);
    }
    return finish(latencies);

    async function finish(latencies) {
    const ok = latencies.filter((ms) => ms >= 0).sort((a, b) => a - b);
    if (ok.length === 0) throw new Error("no keystroke produced a visible change");
    const pick = (q) => +ok[Math.min(ok.length - 1, Math.floor(q * ok.length))].toFixed(1);
    const latency = { trials, missed: trials - ok.length, medianMs: pick(0.5), p95Ms: pick(0.95) };
    console.log(JSON.stringify({ latency }));

    const display = "DISPLAY=:" + args.display;
    const scenes = [];
    inGuest("pkill -x mpv; pkill -f '[p]v -qL' ; true");
    scenes.push(await scene(page, "idle"));
    inGuest(`${display} nohup xfce4-terminal --maximize -x sh -c 'while :; do cat /usr/share/common-licenses/GPL-3; done | pv -qL 40000' >/dev/null 2>&1 &`);
    scenes.push(await scene(page, "scrolling text"));
    await page.screenshot({ path: `${args.name}-scroll.png` });
    inGuest("pkill -f '[p]v -qL'; true");
    inGuest(`${display} nohup mpv --really-quiet --loop --geometry=1280x720 /tmp/hb-test.mp4 >/dev/null 2>&1 &`);
    scenes.push(await scene(page, "video 1280x720"));
    await page.screenshot({ path: `${args.name}-video.png` });
    inGuest("pkill -x mpv; true");

    fs.writeFileSync(args.out, JSON.stringify({ name: args.name, method: args.method ?? "canvas", latency, scenes }, null, 2));
    await browser.close();
    server.close();
    }
}

main().catch((error) => { console.error(error); process.exit(1); });
