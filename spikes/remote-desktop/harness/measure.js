// Measures a browser remote-desktop client through a counting TCP proxy.
//
//   node measure.js --name NAME --target PORT --path /vnc.html?... \
//                   --guest "box ssh desk --" --display 1 --out results.json
//
// PORT is the Mac-side end of an SSH local forward to the guest's loopback
// web client. The proxy counts every byte the client and server exchange.
"use strict";

const net = require("node:net");
const fs = require("node:fs");
const { execFileSync } = require("node:child_process");
const { chromium } = require("playwright");
const jpeg = require("jpeg-js");

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
            if (args.nodelay !== "false") {
                client.setNoDelay(true);
                upstream.setNoDelay(true);
            }
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

async function scene(page, label) {
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
    const browser = await chromium.launch();
    const display = "DISPLAY=:" + args.display;

    try {
        const page = await browser.newPage({ viewport: { width: 1600, height: 900 } });
        if (args.storage) {
            await page.addInitScript((settings) => {
                for (const [key, value] of Object.entries(settings)) localStorage.setItem(key, value);
            }, JSON.parse(args.storage));
        }
        await page.goto(`http://127.0.0.1:${server.address().port}${args.path}`);
        await page.waitForFunction(() => [...document.querySelectorAll("canvas")]
            .some((canvas) => canvas.width > 800 && canvas.height > 500));
        inGuest(`${display} xdotool search --name '^HomeboxLatency$' windowclose || true`);
        await page.waitForTimeout(4000);
        await page.screenshot({ path: `${args.name}-desktop.png` });

        startLatencyTarget(display);
        await page.waitForTimeout(1000);
        await page.mouse.move(800, 450);
        const method = args.method ?? "screenshot";
        const samples = method === "canvas" ? await canvasLatencies(page) : await screenshotLatencies(page);
        const observed = samples.filter((ms) => ms >= 0).sort((a, b) => a - b);
        if (observed.length === 0) throw new Error("No keystroke produced a visible change");
        const middle = Math.floor(observed.length / 2);
        const latency = {
            trials,
            missed: trials - observed.length,
            medianMs: +((observed[Math.floor((observed.length - 1) / 2)] + observed[middle]) / 2).toFixed(1),
            p95Ms: +observed[Math.ceil(0.95 * observed.length) - 1].toFixed(1),
        };
        console.log(JSON.stringify({ latency }));
        const scenes = sceneSeconds === 0 ? [] : await measureScenes(page, display);
        if (args.out) fs.writeFileSync(args.out, JSON.stringify({ name: args.name, method, latency, scenes }, null, 2));
    } finally {
        await browser.close();
        server.close();
    }
}

function startLatencyTarget(display) {
    const terminal = String.raw`stty -echo; printf '\033[2J\033[H\033[?25l'
while IFS= read -rsn1 key; do
    [ "$key" = x ] || continue
    if [ "$lit" = yes ]; then
        lit=no; printf '\033[H\033[49m      '
    else
        lit=yes; printf '\033[H\033[107m      \033[0m'
    fi
done`;
    const encoded = Buffer.from(terminal).toString("base64");
    inGuest(`printf %s '${encoded}' | base64 -d >/tmp/hb-latency.sh;
        ${display} nohup xfce4-terminal --disable-server --maximize --title=HomeboxLatency -x bash /tmp/hb-latency.sh >/dev/null 2>&1 &`);
}

async function canvasLatencies(page) {
    await page.evaluate(() => {
        const screen = [...document.querySelectorAll("canvas")]
            .sort((a, b) => b.width * b.height - a.width * a.height)[0];
        const sample = document.createElement("canvas");
        sample.width = 100;
        sample.height = 100;
        const context = sample.getContext("2d", { willReadFrequently: true });
        window.hashTarget = () => {
            context.drawImage(screen, 0, 25, 200, 200, 0, 0, 100, 100);
            const pixels = context.getImageData(0, 0, 100, 100).data;
            let hash = 0;
            for (let index = 0; index < pixels.length; index += 4) {
                hash = (hash * 31 + pixels[index]) | 0;
            }
            return hash;
        };
    });

    const latencies = [];
    for (let index = 0; index < trials; index++) {
        await page.waitForTimeout(250 + Math.random() * 100);
        await page.evaluate(() => {
            const baseline = window.hashTarget();
            const sent = performance.now();
            window.canvasChanged = new Promise((resolve) => {
                function poll() {
                    if (window.hashTarget() !== baseline) return resolve(performance.now() - sent);
                    if (performance.now() - sent >= 2000) return resolve(-1);
                    requestAnimationFrame(poll);
                }
                requestAnimationFrame(poll);
            });
        });
        await page.keyboard.press("x");
        latencies.push(await page.evaluate(() => window.canvasChanged));
    }
    return latencies;
}

async function measureScenes(page, display) {
    inGuest("pkill -x mpv; pkill -f '[p]v -qL'; true");
    const idle = await scene(page, "idle");
    inGuest(`${display} nohup xfce4-terminal --maximize -x sh -c 'while :; do cat /usr/share/common-licenses/GPL-3; done | pv -qL 40000' >/dev/null 2>&1 &`);
    try {
        const scroll = await scene(page, "scrolling text");
        await page.screenshot({ path: `${args.name}-scroll.png` });
        inGuest("pkill -f '[p]v -qL'; true");
        inGuest(`${display} nohup mpv --really-quiet --loop --geometry=1280x720 /tmp/hb-test.mp4 >/dev/null 2>&1 &`);
        const video = await scene(page, "video 1280x720");
        await page.screenshot({ path: `${args.name}-video.png` });
        return [idle, scroll, video];
    } finally {
        inGuest("pkill -x mpv; pkill -f '[p]v -qL'; true");
    }
}

async function screenshotLatencies(page) {
    const cdp = await page.context().newCDPSession(page);
    const clip = { x: 0, y: 0, width: 300, height: 240, scale: 0.5 };
    async function capture() {
        const { data } = await cdp.send("Page.captureScreenshot", {
            format: "jpeg", quality: 75, clip, optimizeForSpeed: true,
        });
        const at = performance.now();
        const pixels = jpeg.decode(Buffer.from(data, "base64"), { useTArray: true }).data;
        return { pixels, at };
    }

    const dark = (await capture()).pixels;
    await page.keyboard.press("x");
    await page.waitForTimeout(500);
    const light = (await capture()).pixels;
    const marker = [];
    for (let index = 0; index < light.length; index += 4) {
        if (light[index] - dark[index] > 120) marker.push(index);
    }
    if (marker.length < 20) throw new Error(`Latency target not visible (${marker.length} changed pixels)`);
    const brightness = (pixels) => marker.reduce((sum, index) => sum + pixels[index], 0) / marker.length;
    const threshold = (brightness(dark) + brightness(light)) / 2;
    const latencies = [];
    let expectLight = false;
    for (let index = 0; index < trials; index++) {
        await page.waitForTimeout(250 + Math.random() * 100);
        const sent = performance.now();
        await page.keyboard.press("x");
        let elapsed = -1;
        while (performance.now() - sent < 2000) {
            const { pixels, at } = await capture();
            if ((brightness(pixels) >= threshold) === expectLight) {
                elapsed = at - sent;
                break;
            }
        }
        latencies.push(elapsed);
        expectLight = elapsed < 0 ? brightness((await capture()).pixels) < threshold : !expectLight;
    }
    await cdp.detach();
    return latencies;
}

main().catch((error) => { console.error(error); process.exit(1); });
