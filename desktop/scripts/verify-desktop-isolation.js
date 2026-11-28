const { spawn } = require("node:child_process");
const { mkdtempSync, rmSync } = require("node:fs");
const { createServer } = require("node:http");
const { tmpdir } = require("node:os");
const { join } = require("node:path");
const { once } = require("node:events");

async function main() {
    const scratch = mkdtempSync(join(tmpdir(), "homebox-viewer-"));
    let outsideRequests = 0;
    const outside = createServer((_, response) => {
        outsideRequests++;
        response.end("outside viewer");
    });
    const guest = createServer((request, response) => {
        const external = `http://localhost:${outside.address().port}`;
        if (request.url === "/redirect") {
            response.writeHead(302, { Location: external });
            response.end();
            return;
        }
        response.writeHead(200, {
            "Content-Type": "text/html",
            "Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; frame-src 'none'; sandbox allow-scripts allow-same-origin",
        });
        response.end(`<!doctype html><meta charset="utf-8"><button id="probe">Run isolation checks</button>
            <div id="selection" contenteditable>Desktop isolation fixture</div>
            <script>
            document.querySelector("#probe").onclick = async () => {
                window.probeStarted = true;
                const failures = [];
                if (!navigator.userActivation.isActive) failures.push("missing user gesture");
                try { window.wails.Call.ByName("main.Local.ListStatus"); failures.push("binding"); } catch {}
                try { window.webkit.messageHandlers.external.postMessage("viewer isolation fixture"); failures.push("external handler"); } catch {}
                if (window._wails || window._wailsInvoke) failures.push("runtime global");
                try { await fetch("wails://wails/wails/runtime"); failures.push("runtime URL"); } catch {}
                const selection = document.querySelector("#selection");
                selection.focus();
                getSelection().selectAllChildren(selection);
                try { await navigator.clipboard.writeText(selection.textContent); failures.push("async clipboard"); } catch {}
                if (document.execCommand("copy")) failures.push("legacy clipboard");
                const frame = document.createElement("iframe");
                document.body.append(frame);
                if (frame.contentDocument?.execCommand("copy")) failures.push("frame legacy clipboard");
                try { await frame.contentWindow.navigator.clipboard.writeText(selection.textContent); failures.push("frame async clipboard"); } catch {}
                try { await navigator.share({ text: selection.textContent }); failures.push("share"); } catch {}
                if (window.open(${JSON.stringify(external)})) failures.push("popup");
                location.href = ${JSON.stringify(external)};
                await new Promise((resolve) => setTimeout(resolve, 250));
                location.href = "/redirect";
                await new Promise((resolve) => setTimeout(resolve, 250));
                window.result = failures.length ? failures.join(", ") : "PASS";
            };
            </script>`);
    });
    try {
        await Promise.all([outside, guest].map((server) => {
            server.listen(0, "127.0.0.1");
            return once(server, "listening");
        }));
        const binary = join(scratch, "verify");
        const compile = spawn("xcrun", ["clang", "-fobjc-arc", "-framework", "AppKit", "-framework", "WebKit",
            "-framework", "ApplicationServices", join(__dirname, "verify-desktop-isolation.m"),
            join(__dirname, "../desktop_view_darwin.m"), "-o", binary], { stdio: "inherit" });
        if ((await once(compile, "exit"))[0] !== 0) throw new Error("Could not build the isolation fixture");
        const test = spawn(binary, [`http://127.0.0.1:${guest.address().port}/`], { stdio: "inherit" });
        if ((await once(test, "exit"))[0] !== 0 || outsideRequests !== 0) {
            throw new Error(`Viewer isolation failed. Outside requests: ${outsideRequests}`);
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
