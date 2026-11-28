const { spawn, spawnSync } = require("node:child_process");
const { existsSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } = require("node:fs");
const { dirname, join, resolve } = require("node:path");
const { homedir } = require("node:os");
const { once } = require("node:events");

function cleanupState(state, binary, env) {
    // State belongs exclusively to this run, including any interrupted base build.
    const names = new Set();
    for (const [directory, record] of [["boxes", "box.json"], ["lima", "lima.yaml"]]) {
        const directoryPath = join(state, directory);
        if (!existsSync(directoryPath)) continue;
        for (const name of readdirSync(directoryPath)) {
            if (existsSync(join(directoryPath, name, record))) names.add(name);
        }
    }
    for (const name of names) {
        if (spawnSync(join(dirname(binary), "box"), ["rm", "-f", name], { env, stdio: "inherit" }).status !== 0) {
            throw new Error(`VM cleanup failed. State kept at ${state}`);
        }
    }
    rmSync(state, { recursive: true, force: true });
}

async function main() {
    if (!process.argv[2]) throw new Error("Pass the directory for the screenshots.");
    const output = resolve(process.argv[2]);
    const binary = resolve(process.argv[3] ?? join(__dirname, "../dist/homebox-linux/homebox"));
    mkdirSync(output, { recursive: true });
    const lifecycle = process.argv[4] === "--create-delete";
    const vm = lifecycle ? undefined : process.argv[4];
    const lifecycleVM = "linux-lifecycle";
    if (vm && !process.env.HOMEBOX_HOME) throw new Error("Pass HOMEBOX_HOME for the disposable VM.");
    const scratch = mkdtempSync(join(output, ".homebox-linux-"));
    const state = lifecycle ? mkdtempSync(join(homedir(), ".hb-")) : join(scratch, "state");
    const env = {
        ...process.env,
        GDK_BACKEND: "x11",
        XDG_CACHE_HOME: join(scratch, "cache"),
        XDG_CONFIG_HOME: join(scratch, "config"),
        XDG_DATA_HOME: join(scratch, "data"),
        HOMEBOX_HOME: vm ? resolve(process.env.HOMEBOX_HOME) : state,
        HOMEBOX_DESKTOP_SETTINGS: join(scratch, "settings.json"),
        HOMEBOX_DESKTOP_SSH_CONFIG: join(scratch, "ssh"),
        HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS: join(scratch, "pins"),
    };
    // A local-only alias on a closed port exercises SSH failure without a VM.
    if (vm && process.env.HOMEBOX_DESKTOP_SSH_CONFIG && process.env.HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS) {
        env.HOMEBOX_DESKTOP_SSH_CONFIG = process.env.HOMEBOX_DESKTOP_SSH_CONFIG;
        env.HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS = process.env.HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS;
    } else {
        writeFileSync(env.HOMEBOX_DESKTOP_SSH_CONFIG, "Host ssh-fixture\n    HostName 127.0.0.1\n    Port 1\n");
        writeFileSync(env.HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS, "");
    }
    const app = spawn(binary, [], { env, stdio: "inherit" });
    const exited = once(app, "close");
    const mode = lifecycle ? ["--create-delete", binary, lifecycleVM] : vm ? [vm, binary] : [];
    const test = spawn("/usr/bin/python3", [join(__dirname, "verify-linux.py"), String(app.pid), output, ...mode], {
        env, stdio: "inherit",
    });
    const interrupt = () => test.kill();
    process.once("SIGINT", interrupt);
    process.once("SIGTERM", interrupt);
    try {
        const result = await Promise.race([once(test, "close"), exited.then(() => [null])]);
        if (result[0] !== 0) throw new Error("Linux desktop smoke failed.");
    } finally {
        test.kill();
        process.off("SIGINT", interrupt);
        process.off("SIGTERM", interrupt);
        app.kill();
        try {
            await exited;
        } finally {
            rmSync(scratch, { recursive: true, force: true });
            if (lifecycle) cleanupState(state, binary, env);
        }
    }
}

main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
});
