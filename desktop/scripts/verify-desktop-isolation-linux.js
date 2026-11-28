const { spawn, spawnSync } = require("node:child_process");
const { once } = require("node:events");
const { mkdirSync, mkdtempSync, rmSync } = require("node:fs");
const { createServer } = require("node:http");
const { join, resolve } = require("node:path");

function run(command, args) {
    const result = spawnSync(command, args, { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });
    if (result.error) throw result.error;
    if (result.status !== 0) throw new Error(`${command} failed`);
    return result.stdout.trim();
}

async function main() {
    const output = process.argv[2];
    if (!output || !process.argv.includes("--replace-clipboard")) {
        throw new Error("Pass a scratch directory and --replace-clipboard. Use a test X display to preserve your clipboard.");
    }
    mkdirSync(resolve(output), { recursive: true });
    const scratch = mkdtempSync(join(resolve(output), ".viewer-"));
    let outsideRequests = 0;
    let redirects = 0;
    let attachments = 0;
    const outside = createServer((_, response) => {
        outsideRequests++;
        response.end("outside viewer");
    });
    const guest = createServer((request, response) => {
        const external = `http://127.0.0.1:${outside.address().port}/`;
        if (request.url === "/redirect") {
            redirects++;
            response.writeHead(302, { Location: external });
            response.end();
            return;
        }
        if (request.url === "/attachment") {
            attachments++;
            response.writeHead(200, { "Content-Type": "text/html", "Content-Disposition": 'attachment; filename="fixture.html"' });
            response.end("<script>document.title = 'FAIL: rendered attachment'</script>");
            return;
        }
        if (request.url !== "/") {
            response.writeHead(404).end();
            return;
        }
        response.setHeader("Content-Type", "text/html");
        // Exercise the native policies independently of the proxy's CSP.
        response.end(`<!doctype html><meta charset="utf-8">
            <button id="probe" style="padding: 24px">Run isolation checks</button>
            <div id="selection" contenteditable>Desktop isolation fixture</div>
            <script>
            document.title = localStorage.getItem("fixture") || document.cookie
                ? "FAIL: persisted storage" : "READY";
            document.querySelector("#probe").onclick = async () => {
                const failures = [];
                if (!navigator.userActivation.isActive) failures.push("missing user gesture");
                if (window.wails || window._wails || window._wailsInvoke) failures.push("Wails global");
                try { window.webkit.messageHandlers.external.postMessage("fixture"); failures.push("external handler"); } catch {}
                try { await fetch("wails://wails/wails/runtime"); failures.push("runtime URL"); } catch {}
                const selection = document.querySelector("#selection");
                selection.focus();
                getSelection().selectAllChildren(selection);
                try { await navigator.clipboard.writeText(selection.textContent); failures.push("async clipboard write"); } catch {}
                try { await navigator.clipboard.readText(); failures.push("async clipboard read"); } catch {}
                if (document.execCommand("copy") || document.execCommand("paste")) failures.push("legacy clipboard");
                try { await navigator.share({text: selection.textContent}); failures.push("share"); } catch {}
                const frame = document.createElement("iframe");
                document.body.append(frame);
                if (frame.contentDocument?.execCommand("copy")) failures.push("frame clipboard");
                if (window.open("/popup")) failures.push("popup");
                localStorage.setItem("fixture", "saved");
                document.cookie = "fixture=saved";
                if (!localStorage.getItem("fixture") || !document.cookie) failures.push("storage was never written");
                const wait = () => new Promise(resolve => setTimeout(resolve, 450));
                location.href = ${JSON.stringify(external)};
                await wait();
                location.href = "/redirect";
                await wait();
                location.href = "/attachment";
                await wait();
                document.title = failures.length ? "FAIL: " + failures.join(", ") : "PASS";
            };
            </script>`);
    });
    try {
        await Promise.all([outside, guest].map((server) => {
            const listening = once(server, "listening");
            server.listen(0, "127.0.0.1");
            return listening;
        }));
        const binary = join(scratch, "verify");
        const flags = run("pkg-config", ["--cflags", "--libs", "gtk4", "webkitgtk-6.0", "x11"]);
        run("cc", [join(__dirname, "verify-desktop-isolation-linux.c"), "-Wno-deprecated-declarations",
            "-o", binary, ...flags.split(/\s+/), "-ldl"]);
        const child = spawn(binary, [`http://127.0.0.1:${guest.address().port}/`], {
            env: { ...process.env, GDK_BACKEND: "x11", XDG_DATA_HOME: scratch, XDG_CACHE_HOME: scratch },
            stdio: "inherit",
        });
        if ((await once(child, "exit"))[0] !== 0 || outsideRequests || redirects !== 1 || attachments !== 1) {
            throw new Error(`Viewer isolation failed. Outside requests: ${outsideRequests}. Redirects: ${redirects}. Attachments: ${attachments}.`);
        }
    } finally {
        outside.close();
        guest.close();
        rmSync(scratch, { recursive: true, force: true });
    }
}

main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
});
