const { mkdtempSync, rmSync } = require("node:fs");
const { homedir } = require("node:os");
const { join, resolve } = require("node:path");
const { spawnSync } = require("node:child_process");
const { build } = require("./build");

const screenshot = resolve(process.argv[2] ?? join(__dirname, "../dist/homebox-status.png"));
const bundle = build();
const scratch = mkdtempSync(join(homedir(), ".homebox-desktop-spike-"));
try {
    const result = spawnSync("swift", [join(__dirname, "verify.swift"), bundle, scratch, screenshot], {
        stdio: "inherit",
    });
    if (result.error) throw result.error;
    process.exitCode = result.status ?? 1;
} finally {
    rmSync(scratch, { recursive: true, force: true });
}
