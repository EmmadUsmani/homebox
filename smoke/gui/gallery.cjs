const fs = require("node:fs");
const path = require("node:path");

const [artifacts, ...logs] = process.argv.slice(2);
const results = new Map();
for (const log of logs) {
    for (const line of fs.readFileSync(log, "utf8").split("\n")) {
        const result = line.match(/^(PASS|FAIL) (.+)$/);
        if (result) results.set(result[2], result[1]);
    }
}

const checks = [
    ["Capture the full KasmVNC desktop", "kasmvnc-desktop"],
    ["GTK4 C window paints pixels", "SmokeGTK"],
    ["Qt 6 C++ window paints pixels", "SmokeQt"],
    ["Tkinter window paints pixels", "SmokeTk"],
    ["PySide6 window paints pixels", "SmokePySide"],
    ["SDL2 window paints pixels", "SmokeSDL"],
    ["Swing window paints pixels", "SmokeSwing"],
    ["Electron window paints pixels", "SmokeElectron"],
    ["OpenGL triangle on the desktop reads back foreground and background pixels", "opengl-desktop"],
    ["Surfaceless EGL triangle reads back pixels without X", "egl-offscreen"],
    ["Cairo PNG and ImageMagick transform preserve expected colors", "cairo", "cairo-resized"],
    ["Noto fallback and Pango render CJK, emoji and shaped Arabic", "fonts"],
    ["FFmpeg encodes and decodes 24 VP8 frames", "ffmpeg-frame"],
];
for (const engine of ["chromium", "firefox", "webkit"]) {
    for (const mode of ["headless", "headed"]) {
        const image = `playwright-${engine}-${mode}`;
        const images = engine === "chromium" ? [image, image + "-webgl", image + "-video"] : [image];
        checks.push([`Playwright ${engine} ${mode} renders and responds to clicks`, ...images]);
    }
}
checks.push(
    ["Host Playwright sees GUI pixels through the one-time desktop URL", "remote-before-input"],
    ["desktop browser keyboard and click reach the GUI", "remote-after-input"],
    ["guest clipboard helper exchanges UTF-8 with a GUI app", "remote-clipboard"],
);

const missing = [];
const cards = checks.map(([label, ...images]) => {
    const status = results.get(label) || "FAIL";
    const figures = images.map((image) => {
        const file = image + ".png";
        const exists = fs.existsSync(path.join(artifacts, file));
        if (!exists && status === "PASS") missing.push(file);
        const preview = exists
            ? `<a href="${file}"><img src="${file}" alt="${image}" loading="lazy"></a>`
            : "<p>Capture unavailable.</p>";
        return `<figure>${preview}<figcaption>${file}</figcaption></figure>`;
    }).join("\n");
    const detail = results.has(label) ? "" : "<p>No completed check result.</p>";
    return `<section><h2><b class="${status}">${status}</b> ${label}</h2>${detail}${figures}</section>`;
});

fs.writeFileSync(path.join(artifacts, "index.html"), `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Homebox GUI smoke evidence</title>
<style>
body { background: #f1f4f7; color: #1a2230; font: 16px system-ui, sans-serif; margin: 2rem auto; max-width: 80rem; padding: 0 1rem; }
main { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr)); gap: 1rem; align-items: start; }
section { background: white; border: 1px solid #ccd5df; border-radius: .5rem; padding: 1rem; }
h2 { font-size: 1rem; margin: 0 0 1rem; }
.PASS { color: #13683b; }
.FAIL { color: #a62727; }
figure { margin: 0 0 .75rem; }
img { display: block; max-width: 100%; height: auto; }
figcaption { color: #465365; font-size: .85rem; margin-top: .25rem; }
</style>
<h1>Homebox GUI smoke evidence</h1>
<main>
${cards.join("\n")}
</main>
</html>
`);

if (missing.length) {
    console.error("Missing captures for passing GUI checks: " + missing.join(", "));
    process.exitCode = 1;
}
