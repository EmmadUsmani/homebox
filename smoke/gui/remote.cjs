const assert = require("node:assert/strict");
const { execFileSync, spawn } = require("node:child_process");
const path = require("node:path");
const { once } = require("node:events");
const { createInterface } = require("node:readline");
const { setTimeout: delay } = require("node:timers/promises");
const [modules, box, vm, artifacts] = process.argv.slice(2);
const { chromium } = require(modules + "/playwright");
const jpeg = require(modules + "/jpeg-js");
const directory = "homebox-smoke/gui";
const clipboard = "/usr/local/lib/homebox-desktop/clipboard";
const width = 1600;
const height = 900;

function guest(command, input) {
    return execFileSync(box, ["ssh", vm, "--", command], {
        input, encoding: "utf8", timeout: 12000, stdio: ["pipe", "pipe", "pipe"],
    }).trim();
}

async function until(label, probe) {
    for (let attempt = 0; attempt < 25; attempt++) {
        const result = await probe();
        if (result) return result;
        await delay(200);
    }
    throw new Error(label + " did not appear");
}

async function windowOrigin(title) {
    const geometry = await until(title, () => guest(`cd ~/${directory}; . ./session.sh;
        id=$(xdotool search --onlyvisible --name '^${title}$' | head -n 1);
        test -z "$id" || xwininfo -id "$id"`));
    const coordinate = (axis) => Number(geometry.match(
        new RegExp(`Absolute upper-left ${axis}:\\s+(-?\\d+)`))[1]);
    return [coordinate("X"), coordinate("Y")];
}

async function run() {
    const proxy = spawn(box, ["desktop", vm, "--no-open"], { stdio: ["ignore", "pipe", "ignore"] });
    const exited = once(proxy, "exit");
    const lines = createInterface({ input: proxy.stdout });
    let browser;
    let stage = "starting desktop proxy";
    try {
        const [result] = await Promise.race([
            once(lines, "line"),
            exited.then(() => []),
            delay(40000, [], { ref: false }),
        ]);
        if (!result || new URL(result).hostname !== "127.0.0.1") throw new Error("missing loopback URL");

        stage = "connecting through the browser";
        browser = await chromium.launch({ chromiumSandbox: true });
        const page = await browser.newPage({ viewport: { width, height } });
        page.setDefaultTimeout(12000);
        await page.goto(result);
        const canvas = page.locator(`canvas[width="${width}"][height="${height}"]:visible`).first();
        await canvas.waitFor();

        stage = "launching the fixture from the Xfce terminal";
        guest(`cd ~/${directory}; rm -f remote-result; . ./session.sh;
            nohup xfce4-terminal --disable-server --title=HomeboxSmokeTerminal --geometry=82x20+20+40 \
            >/dev/null 2>&1 </dev/null &`);
        const click = async (x, y) => {
            const desktop = await canvas.boundingBox();
            assert.ok(desktop);
            await page.mouse.click(desktop.x + x * desktop.width / width,
                desktop.y + y * desktop.height / height, { delay: 100 });
        };
        const [terminalX, terminalY] = await windowOrigin("HomeboxSmokeTerminal");
        await click(terminalX + 100, terminalY + 70);
        await page.keyboard.type(`cd ~/${directory} && /usr/bin/python3 remote.py`);
        await page.keyboard.press("Enter");
        const [x, y] = await windowOrigin("Homebox GUI Smoke");

        stage = "checking the browser framebuffer";
        await until("fixture paint in the browser framebuffer", async () => {
            const image = jpeg.decode(await canvas.screenshot({ type: "jpeg" }));
            const pixel = (Math.floor((y + 175) * image.height / height) * image.width +
                Math.floor((x + 25) * image.width / width)) * 4;
            return image.data[pixel] < 40 && image.data[pixel + 1] > 170;
        });
        await canvas.screenshot({ path: path.join(artifacts, "remote-before-input.png") });
        console.log("PASS Host Playwright sees GUI pixels through the one-time desktop URL");

        stage = "checking remote keyboard and mouse input";
        await click(x + 80, y + 44);
        await page.keyboard.type("remote-typed", { delay: 35 });
        await click(x + 70, y + 100);
        const saved = (value) => until("GUI save marker", () =>
            guest(`cat ~/${directory}/remote-result 2>/dev/null || true`) === value);
        await saved("remote-typed");
        await canvas.screenshot({ path: path.join(artifacts, "remote-after-input.png") });
        console.log("PASS desktop browser keyboard and click reach the GUI");

        stage = "checking the Desktop tab's guest clipboard helper";
        const clipboardText = "Pasted into Tk — café";
        guest(clipboard + " paste", clipboardText);
        await until("clipboard paste", () => guest(clipboard + " copy") === clipboardText);
        await click(x + 170, y + 100);
        await click(x + 70, y + 100);
        await saved(clipboardText);
        guest(clipboard + " paste", "replacement");
        await click(x + 270, y + 100);
        await until("GUI clipboard", () => guest(clipboard + " copy") === clipboardText);
        await canvas.screenshot({ path: path.join(artifacts, "remote-clipboard.png") });
        console.log("PASS guest clipboard helper exchanges UTF-8 with a GUI app");
    } catch (error) {
        // Playwright navigation errors can contain the one-time URL.
        console.error("FAIL remote desktop while " + stage + " (" + error.name + ")");
        process.exitCode = 1;
    } finally {
        try {
            guest(`cd ~/${directory}; . ./session.sh;
                for title in '^Homebox GUI Smoke$' '^HomeboxSmokeTerminal$'; do
                    xdotool search --onlyvisible --name "$title" windowclose 2>/dev/null || true;
                done;
                rm -f remote-result`);
        } finally {
            if (browser) await browser.close();
            lines.close();
            proxy.kill("SIGINT");
            await Promise.race([exited, delay(5000)]);
            if (proxy.exitCode === null) proxy.kill("SIGTERM");
        }
    }
}

run().catch(() => {
    console.error("FAIL remote desktop fixture setup or cleanup");
    process.exitCode = 1;
});
