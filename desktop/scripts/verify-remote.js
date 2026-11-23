const { spawnSync, spawn, execFile } = require("node:child_process");
const { copyFileSync, mkdtempSync, readFileSync, rmSync, writeFileSync, mkdirSync, statSync } = require("node:fs");
const { createServer } = require("node:net");
const { homedir } = require("node:os");
const { join, resolve } = require("node:path");
const { promisify } = require("node:util");
const { build } = require("./build");

function listen(server) {
    return new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(0, "127.0.0.1", () => resolve(server.address().port));
    });
}

async function verify(boxState, screenshots) {
    const bundle = process.env.HOMEBOX_VERIFY_BUNDLE || build();
    const scratch = mkdtempSync(join(homedir(), ".homebox-desktop-remote-"));
    const broken = createServer((socket) => socket.end("This is a test listener, not SSH.\r\n"));
    try {
        const brokenPort = await listen(broken);
        const reserve = createServer();
        const closedPort = await listen(reserve);
        await new Promise((resolve) => reserve.close(resolve));
        const localConfig = readFileSync(join(boxState, "ssh/config"), "utf8");
        const failureConfig = localConfig.replace(/^Host remote-test$/m, "Host wrong-pin").replace(/^  HostKeyAlias .*$/m, "  HostKeyAlias wrong-pin");
        const config = join(scratch, "ssh_config");
        const inventory = readFileSync(join(homedir(), ".ssh/homebox_config"), "utf8");
        writeFileSync(config, `${inventory}\n${failureConfig}
Host unresolved
    HostName homebox-fixture.invalid
Host stopped
    HostName 127.0.0.1
    Port ${closedPort}
Host broken
    HostName 127.0.0.1
    Port ${brokenPort}
Match host added-fixture
    HostName 127.0.0.1
    Port ${closedPort}
`, { mode: 0o600 });
        const pins = join(scratch, "known_hosts");
        copyFileSync(join(homedir(), ".ssh/homebox_known_hosts"), pins);
        const decoy = join(scratch, "decoy");
        const keygen = spawnSync("/usr/bin/ssh-keygen", ["-q", "-t", "ed25519", "-N", "", "-f", decoy]);
        if (keygen.status !== 0) throw new Error("Could not generate a disposable host pin");
        const [keyType, key] = readFileSync(decoy + ".pub", "utf8").trim().split(/\s+/);
        writeFileSync(pins, `\nwrong-pin ${keyType} ${key}\n`, { flag: "a" });
        mkdirSync(screenshots, { recursive: true });

        if (process.env.HOMEBOX_CAPTURE_SSH_FIXTURES) {
            for (const alias of ["unresolved", "stopped", "broken", "wrong-pin"]) {
                const capture = await promisify(execFile)("/usr/bin/ssh", ["-F", config, "-a", "-x", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
                    "-o", "StrictHostKeyChecking=yes", "-o", `UserKnownHostsFile=${pins}`, alias, "true"],
                { encoding: "utf8", env: { ...process.env, LC_ALL: "C" } }).catch((error) => error);
                console.log(alias, capture.code, capture.stderr.replaceAll(scratch, "<scratch>").replace(/SHA256:[^\s]+/g, "<fingerprint>"));
            }
        }

        for (const mode of ["normal", "minimal"]) {
            const output = join(screenshots, mode);
            mkdirSync(output, { recursive: true });
            const settings = join(scratch, `settings-${mode}.json`);
            await new Promise((resolve, reject) => {
                const child = spawn("swift", [join(__dirname, "verify-remote.swift"), bundle, join(scratch, "local"), config, pins,
                    settings, mode, output], { stdio: "inherit" });
                child.once("error", reject);
                child.once("exit", (status) => status === 0 ? resolve() : reject(new Error(`Native verification failed during ${mode} launch`)));
            });
            const saved = JSON.parse(readFileSync(settings, "utf8"));
            if (!saved.remoteHosts.includes("added-fixture") || (statSync(settings).mode & 0o777) !== 0o600) {
                throw new Error("Added host was not saved to an owner-only settings file");
            }
        }
    } finally {
        broken.close();
        rmSync(scratch, { recursive: true, force: true });
    }
}

if (!process.argv[2]) throw new Error("Pass the scratch VM's HOMEBOX_HOME as the first argument");
verify(resolve(process.argv[2]), resolve(process.argv[3] || join(__dirname, "../dist/remote-verification"))).catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
});
