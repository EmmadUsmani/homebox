const { spawn, spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const usage = `Usage:
  scripts/test.sh change|batch|full [--area AREA ...] [--match ERE] [--base REF] [--keep-vm]
  scripts/test.sh last
  scripts/test.sh bisect GOOD BAD change|batch|full [--area AREA ...] [--match ERE]`;
const freshVM = ["--cpus", "2", "--memory", "4", "--disk", "30"];
const tiers = ["change", "batch", "full"];
const root = process.env.HOMEBOX_TEST_CHECKOUT || path.resolve(__dirname, "..");
const history = path.join(root, "docs/test-runs.md");

process.chdir(root);

function capture(command, args, options = {}) {
    return spawnSync(command, args, { encoding: "utf8", ...options });
}

function git(...args) {
    const result = capture("git", args);
    if (result.status !== 0) throw new Error(result.stderr?.trim() || "git failed");
    return result.stdout;
}

function parse(args) {
    const [tier, ...flags] = args;
    if (!tiers.includes(tier)) throw new Error(usage);
    const options = { tier, areas: [], base: "origin/main", match: "", keep: false };
    for (let i = 0; i < flags.length; i++) {
        const flag = flags[i];
        if (flag === "--keep-vm") {
            options.keep = true;
            continue;
        }
        const value = flags[++i];
        if (!value || value.startsWith("--")) throw new Error(usage);
        switch (flag) {
        case "--area": options.areas.push(value); break;
        case "--match": options.match = value; break;
        case "--base": options.base = value; break;
        default: throw new Error(usage);
        }
    }
    if (options.match && ![0, 1].includes(capture("grep", ["-E", "--", options.match], { input: "" }).status)) {
        throw new Error("--match requires a valid extended regular expression");
    }
    return options;
}

function smokeAreas() {
    const smoke = path.join(root, "smoke");
    if (!fs.existsSync(smoke)) return [];
    return fs.readdirSync(smoke).filter(area => /^[a-z0-9-]+$/.test(area) &&
        fs.existsSync(path.join(smoke, area, "test.sh")));
}

function selection(options) {
    const available = smokeAreas();
    const allowed = [...available, "doctor", "e2e", "desktop"];
    const { areas, tier, base, match } = options;
    for (const area of areas) {
        if (!allowed.includes(area)) throw new Error(`Unknown area "${area}". Choose from ${allowed.join(", ")}.`);
    }
    const changed = tier === "change" && !areas.length
        ? git("diff", "--name-only", "--no-renames", "-z", git("merge-base", base, "HEAD").trim(), "--") +
            git("ls-files", "--others", "--exclude-standard", "-z")
        : "";
    const files = changed.split("\0").filter(Boolean);
    const touched = prefix => files.some(file => file.startsWith(prefix));
    const sharedSmoke = files.some(file => /^smoke\/(lib|run)\.sh$/.test(file));
    const guest = touched("guest/");
    const smoke = available.filter(area => areas.length ? areas.includes(area)
        : tier !== "change" || sharedSmoke || touched(`smoke/${area}/`) || (guest && ["os", "runtimes"].includes(area)));
    const profileMatch = path.join(root, "smoke/runtimes/profile-match");
    const runtimeMatch = tier === "change" && guest && !sharedSmoke && !touched("smoke/runtimes/") &&
        fs.existsSync(profileMatch) ? fs.readFileSync(profileMatch, "utf8").trim() : "";
    return {
        smoke, runtimeMatch,
        doctor: areas.length ? areas.includes("doctor") : tier !== "change" || ["cmd/", "internal/"].some(touched),
        e2e: areas.length ? areas.includes("e2e") : tier !== "change" || touched("e2e/"),
        desktop: areas.length ? areas.includes("desktop") : tier === "full" || touched("desktop/"),
        scoped: areas.length > 0 || Boolean(match),
    };
}

async function runSuite(options) {
    const selected = selection(options);
    const { tier, match, keep } = options;
    const level = tier === "full" ? "full" : "quick";
    const revision = git("rev-parse", "--short=12", "HEAD").trim();
    const dirty = git("status", "--porcelain", "--", ".", ":(exclude)docs/test-runs.md").length > 0;
    const started = Date.now();
    const resultsRoot = path.resolve(process.env.SMOKE_RESULTS || ".smoke-results");
    fs.mkdirSync(resultsRoot, { recursive: true });
    const results = fs.mkdtempSync(path.join(resultsRoot, `${tier}-${new Date().toISOString().slice(0, 10)}-`));
    const steps = [];
    let scratch;
    let active;
    let interrupted;
    let vmAttempted = false;
    const vm = "settingsvm";
    const index = Number(process.env.HOMEBOX_TEST_INDEX || 70);
    const sourceHome = process.env.HOMEBOX_HOME || path.join(os.homedir(), ".homebox");
    const env = { ...process.env };
    delete env.HOMEBOX_FIRST_INDEX;
    delete env.HOMEBOX_E2E_INDEX;

    function interrupt(signal) {
        interrupted = signal;
        active?.kill(signal);
    }
    process.on("SIGINT", interrupt);
    process.on("SIGTERM", interrupt);

    const duration = start => `${((Date.now() - start) / 1000).toFixed(1)}s`;
    function report(name, status, elapsed, details = "") {
        steps.push({ name, status });
        console.log(`${status} ${name} (${elapsed})${details ? ` — ${details}` : ""}`);
        return status === "PASS";
    }

    async function command(commandName, args, commandEnv, log, input) {
        const fd = fs.openSync(log, "a", 0o600);
        try {
            return await new Promise(resolve => {
                active = spawn(commandName, args, { env: commandEnv, stdio: [input ? "pipe" : "ignore", fd, fd] });
                if (input) {
                    active.stdin.on("error", () => {});
                    active.stdin.end(input);
                }
                active.on("error", error => fs.writeSync(fd, `${error.message}\n`));
                active.on("close", code => {
                    active = undefined;
                    resolve(code === 0);
                });
            });
        } finally {
            fs.closeSync(fd);
        }
    }

    async function step(name, action, classify = () => "PASS", cleanup = false) {
        if (interrupted && !cleanup) return report(name, "SKIP", "0.0s", "interrupted");
        const start = Date.now();
        const log = path.join(results, `${name}.log`);
        console.log(`Running ${name} (log: ${log})`);
        let status;
        try {
            const ok = await action(log);
            status = ok ? classify(fs.readFileSync(log, "utf8")) : "FAIL";
        } catch (error) {
            fs.appendFileSync(log, `${error.message}\n`);
            status = "FAIL";
        }
        if (status === "FAIL") console.error(fs.readFileSync(log, "utf8").trimEnd().split("\n").slice(-15).join("\n"));
        return report(name, status, duration(start));
    }

    const run = (name, executable, args, commandEnv = env) =>
        step(name, log => command(executable, args, commandEnv, log));
    const box = (name, ...args) => run(name, env.BOX, args);

    console.log(`Selected ${tier} (${level} smoke): ${[
        ...selected.smoke, ...["doctor", "e2e", "desktop"].filter(area => selected[area]),
    ].join(", ") || "static checks only"}`);

    try {
        if ((selected.e2e || selected.doctor || selected.smoke.length || selected.desktop) &&
            (!Number.isInteger(index) || index < 1 || index > 99)) {
            throw new Error("HOMEBOX_TEST_INDEX must be an integer from 1 to 99");
        }
        await step("gofmt", async log =>
            await command("gofmt", ["-l", "."], env, log) && fs.statSync(log).size === 0);
        await run("vet", "go", ["vet", "./..."]);
        await run("unit", "go", ["test", "./..."], { ...env, HOMEBOX_E2E: "0" });
        await step("shellcheck", async log => {
            const files = git("ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "*.sh")
                .split("\0").filter(file => file && fs.existsSync(file));
            if (!files.length) {
                fs.writeFileSync(log, "");
                return true;
            }
            return command("shellcheck", ["-x", "-P", "SCRIPTDIR", ...files], env, log);
        });

        if (selected.e2e) {
            await run("e2e", "go", ["test", "-v", "-count=1", "-timeout=45m", "./e2e/..."], { ...env, HOMEBOX_E2E: "1", HOMEBOX_E2E_INDEX: String(index) });
        }
        if (selected.desktop) {
            const built = await run("desktop-build", "node", ["desktop/scripts/build.js"]);
            if (built && !interrupted) {
                const permission = capture("swift", ["-e", "import ApplicationServices; print(AXIsProcessTrusted())"]);
                if (permission.status !== 0) {
                    report("desktop-accessibility", "FAIL", "0.0s", permission.stderr?.trim() || "Swift probe failed");
                } else if (permission.stdout.trim() !== "true") {
                    report("desktop-verification", "SKIP", "0.0s", "grant Accessibility to the terminal or runner on this Mac");
                } else {
                    selected.desktopUI = true;
                    await run("desktop-viewer", "node", ["desktop/scripts/verify-desktop-isolation.js"]);
                }
            }
        }

        if (selected.smoke.length || selected.doctor || selected.desktopUI) {
            if (process.platform !== "darwin") throw new Error("VM checks require a macOS homebox host");
            if (interrupted) throw new Error("interrupted before VM setup");
            scratch = fs.mkdtempSync(path.join(os.homedir(), ".hb-w-"));
            env.HOMEBOX_HOME = path.join(scratch, "state");
            env.HOMEBOX_FIRST_INDEX = String(index);
            env.BOX = path.join(scratch, "box");
            const ready = await run("box-build", "go", ["build", "-o", env.BOX, "./cmd/box"]) &&
                await box("host-init", "host", "init");
            let booted = false;
            if (ready && !interrupted) {
                const imageArgs = tier === "change" ? await smokeImage() : freshVM;
                if (!interrupted) {
                    vmAttempted = true;
                    booted = await box("vm", "new", vm, ...imageArgs);
                }
            }
            if (!booted) {
                for (const area of [...selected.smoke, ...(selected.doctor ? ["doctor"] : []),
                    ...(selected.desktopUI ? ["desktop-vm"] : [])]) {
                    report(area, "SKIP", "0.0s", "VM unavailable");
                }
            } else {
                if (selected.doctor) await box("doctor", "doctor", vm);
                for (const area of selected.smoke) {
                    await step(area, log => command("smoke/run.sh", [vm, area], {
                        ...env, SMOKE_RESULTS: results, SMOKE_LEVEL: level,
                        SMOKE_MATCH: match || (area === "runtimes" ? selected.runtimeMatch : ""),
                    }, log), output => /^PASS [^\d\n]/m.test(output) ? "PASS" : "SKIP");
                }
                if (selected.desktopUI) await desktopChecks();
            }
        }
    } catch (error) {
        report("runner", "FAIL", duration(started), error.message);
    } finally {
        try {
            if (vmAttempted && keep) {
                console.log(`Kept ${vm}: HOMEBOX_HOME="${env.HOMEBOX_HOME}" "${env.BOX}" rm -f ${vm}`);
            } else if (scratch) {
                const hasVM = ["boxes", "lima"].some(dir => fs.existsSync(path.join(env.HOMEBOX_HOME, dir, vm)));
                const removed = !vmAttempted || !hasVM ||
                    await step("cleanup", log => command(env.BOX, ["rm", "-f", vm], env, log), undefined, true);
                if (removed) fs.rmSync(scratch, { recursive: true, force: true });
                else console.error(`VM state kept for cleanup: ${env.HOMEBOX_HOME}`);
            }
        } catch (error) {
            report("cleanup", "FAIL", duration(started), error.message);
        }
        const totals = Object.fromEntries(["PASS", "FAIL", "SKIP"].map(status =>
            [status, steps.filter(step => step.status === status).length]));
        const outcome = totals.FAIL || interrupted ? "FAIL" : "PASS";
        const model = capture("sysctl", ["-n", "hw.model"]).stdout?.trim() || os.arch();
        const version = capture("sw_vers", ["-productVersion"]).stdout?.trim() || "n/a";
        const areas = [...selected.smoke, ...["doctor", "e2e", "desktop"].filter(area => selected[area])].join(", ") || "static";
        const row = `| ${new Date().toISOString()} | ${revision} | ${dirty ? "yes" : "no"} | ${model} | ${version} | ${tier} | ${selected.scoped ? "partial" : "default"} | ${areas} | ${outcome} | ${totals.PASS}/${totals.FAIL}/${totals.SKIP} | ${duration(started)} |\n`;
        console.log(`\n${row.trimEnd()}\nLogs: ${results}`);
        if (!process.env.HOMEBOX_TEST_CHECKOUT) fs.appendFileSync(history, row);
        process.exitCode = outcome === "FAIL" ? 1 : 0;
        if (process.env.HOMEBOX_TEST_CHECKOUT && match && selected.smoke.length &&
            selected.smoke.every(area => steps.some(step => step.name === area && step.status === "SKIP")) &&
            outcome === "PASS") {
            process.exitCode = 125;
        }
        if (interrupted) process.exitCode = interrupted === "SIGINT" ? 130 : 143;
    }

    async function smokeImage() {
        const image = process.env.HOMEBOX_SMOKE_IMAGE || "smoke";
        const imageEnv = { ...env, HOMEBOX_HOME: sourceHome };
        const list = capture(env.BOX, ["image", "ls"], { env: imageEnv });
        const flags = capture(env.BOX, ["new", "--help"], { env });
        if (list.status !== 0 || !flags.stdout?.includes("--image")) {
            console.warn("Warning: box image commands unavailable. Provisioning a fresh VM.");
        } else if (!list.stdout.split(/\s+/).includes(image)) {
            console.warn(`Warning: saved smoke image "${image}" unavailable. Provisioning a fresh VM.`);
        } else {
            const archive = path.join(scratch, "smoke-image");
            const log = path.join(results, "smoke-image.log");
            if (await command(env.BOX, ["image", "export", image, archive], imageEnv, log) &&
                await command(env.BOX, ["image", "import", archive], env, log)) {
                fs.rmSync(archive);
                return ["--image", image];
            }
            console.warn(`Warning: could not copy the saved smoke image into isolated state. See ${log}. Provisioning a fresh VM.`);
        }
        return freshVM;
    }

    async function desktopChecks() {
        const bundle = path.join(root, "desktop/dist/homebox.app");
        const scripts = path.join(root, "desktop/scripts");
        const artifacts = path.join(results, "desktop");
        fs.mkdirSync(artifacts);
        await run("desktop-files", "swift", [path.join(scripts, "verify-files.swift"), bundle,
            env.HOMEBOX_HOME, artifacts, "normal", `local=${vm}`]);
        if (tier === "full") {
            if (await box("desktop-deps", "ssh", vm, "--", "sudo -n apt-get update && sudo -n apt-get install -y xdotool")) {
                await run("desktop-ui", "swift", [path.join(scripts, "verify-desktop.swift"), bundle,
                    env.HOMEBOX_HOME, vm, artifacts]);
            }
            // Settings changes the scratch VM's SSH host keys, so run it last.
            await run("desktop-settings", "swift", [path.join(scripts, "verify-settings.swift"), bundle,
                env.HOMEBOX_HOME, vm, artifacts, String(20000 + 100 * index)]);
        }
    }
}

function last() {
    const rows = fs.readFileSync(history, "utf8").trimEnd().split("\n")
        .map(row => row.split("|").map(field => field.trim()));
    for (const tier of tiers) {
        const passes = rows.filter(row => row[6] === tier && row[9] === "PASS");
        for (const scope of ["default", "partial"]) {
            const latest = passes.findLast(row => row[7] === scope);
            if (latest) {
                console.log(`${tier} (${scope})  ${latest[1]}  ${latest[2]}${latest[3] === "yes" ? " (dirty)" : ""}  ${latest[11]}`);
            } else if (scope === "default") {
                console.log(`${tier}  no passing default run recorded`);
            }
        }
    }
}

function bisect([good, bad, ...args]) {
    if (!good || !bad) throw new Error(usage);
    const options = parse(args);
    if (options.keep) throw new Error("--keep-vm is unavailable during bisect");
    const revisions = [good, bad].map(ref => git("rev-parse", "--verify", "--end-of-options", `${ref}^{commit}`).trim());
    const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "homebox-bisect-"));
    const checkout = path.join(scratch, "checkout");
    const runner = path.join(scratch, "runner.js");
    fs.copyFileSync(__filename, runner);
    try {
        git("worktree", "add", "--quiet", "--detach", checkout, revisions[1]);
        const env = { ...process.env, HOMEBOX_TEST_CHECKOUT: checkout,
            SMOKE_RESULTS: path.resolve(process.env.SMOKE_RESULTS || ".smoke-results") };
        const run = args => capture("git", ["-C", checkout, "bisect", ...args], { stdio: "inherit", env });
        const start = run(["start", revisions[1], revisions[0]]);
        if (start.status !== 0) throw new Error("could not start git bisect");
        const base = options.tier === "change" && !args.includes("--base") ? ["--base", revisions[0]] : [];
        const result = run(["run", process.execPath, runner, ...args, ...base]);
        if (result.status !== 0) throw new Error("git bisect could not identify a unique first bad commit");
    } finally {
        if (fs.existsSync(checkout)) git("worktree", "remove", "--force", checkout);
        fs.rmSync(scratch, { recursive: true, force: true });
    }
}

async function main(args) {
    if (args[0] === "last" && args.length === 1) return last();
    if (args[0] === "bisect") return bisect(args.slice(1));
    await runSuite(parse(args));
}

main(process.argv.slice(2)).catch(error => {
    console.error(error.message);
    process.exitCode = process.env.HOMEBOX_TEST_CHECKOUT ? 125 : 2;
});
