const { spawn } = require("node:child_process");
const { mkdirSync, mkdtempSync, rmSync, writeFileSync } = require("node:fs");
const { join, resolve } = require("node:path");
const { once } = require("node:events");

async function main() {
    if (!process.argv[2]) throw new Error("Pass the directory for the screenshots.");
    const output = resolve(process.argv[2]);
    const binary = resolve(process.argv[3] ?? join(__dirname, "../dist/homebox-linux/homebox"));
    mkdirSync(output, { recursive: true });
    const vm = process.argv[4];
    if (vm && !process.env.HOMEBOX_HOME) throw new Error("Pass HOMEBOX_HOME for the disposable VM.");
    const scratch = mkdtempSync(join(output, ".homebox-linux-"));
    const env = {
        ...process.env,
        GDK_BACKEND: "x11",
        XDG_CACHE_HOME: join(scratch, "cache"),
        XDG_CONFIG_HOME: join(scratch, "config"),
        XDG_DATA_HOME: join(scratch, "data"),
        HOMEBOX_HOME: vm ? resolve(process.env.HOMEBOX_HOME) : join(scratch, "state"),
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
    const exited = once(app, "exit");
    try {
        const test = spawn("/usr/bin/python3", [join(__dirname, "verify-linux.py"), String(app.pid), output, ...(vm ? [vm, binary] : [])], {
            env, stdio: "inherit",
        });
        if ((await once(test, "exit"))[0] !== 0) throw new Error("Linux desktop smoke failed.");
    } finally {
        app.kill();
        try {
            await exited;
        } finally {
            rmSync(scratch, { recursive: true, force: true });
        }
    }
}

main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
});
