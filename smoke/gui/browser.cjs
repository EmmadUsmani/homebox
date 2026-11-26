const assert = require("node:assert/strict");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const [playwrightPath, engine, mode, videoFile] = process.argv.slice(2);
const playwright = require(playwrightPath);

async function run() {
    const options = { headless: mode === "headless" };
    if (engine === "chromium") {
        // Use the guest profile's Playwright default for this generated local page.
        options.args = ["--use-gl=angle", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"];
    }
    const browser = await playwright[engine].launch(options);
    try {
        const page = await browser.newPage();
        await page.setContent(`
            <body style="background:#0044cc">
                <button onclick="this.textContent = 'clicked'">click me</button>
            </body>
        `);
        await page.getByRole("button", { name: "click me" }).click();
        assert.equal(await page.getByRole("button").textContent(), "clicked");
        const screenshot = await page.screenshot();
        const [blue, red] = execFileSync("magick", [
            "png:-", "-format", "%[fx:p{200,150}.b] %[fx:p{200,150}.r]", "info:",
        ], { input: screenshot, encoding: "utf8", timeout: 5000 }).trim().split(/\s+/).map(Number);
        assert.ok(blue > 0.7 && red < 0.2, "Screenshot is missing the blue page background");

        if (engine === "chromium") {
            await page.evaluate(() => {
                const gl = document.createElement("canvas").getContext("webgl");
                if (!gl) throw new Error("WebGL context unavailable");
                gl.clearColor(0, 1, 0, 1);
                gl.clear(gl.COLOR_BUFFER_BIT);
                const pixel = new Uint8Array(4);
                gl.readPixels(0, 0, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, pixel);
                if (String(pixel) !== "0,255,0,255") throw new Error(`Unexpected WebGL pixel ${pixel}`);
            });
            await page.goto(new URL(`file://${path.resolve(videoFile)}`).href);
            await page.locator("video").evaluate(async (video) => {
                video.muted = true;
                await video.play();
            });
            await page.waitForFunction(() => document.querySelector("video").currentTime > 0.2);
        }
    } finally {
        await browser.close();
    }
}

run().catch((error) => {
    console.error(error);
    process.exitCode = 1;
});
