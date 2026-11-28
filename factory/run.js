"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { setTimeout: delay } = require("node:timers/promises");

const control = __dirname;
const repo = path.resolve(process.argv[2] || path.join(control, "..", "factory-project"));
const ownerFiles = ["MISSION.md", "STEERING.md"];
const once = process.argv[3] === "--once";
const hours = Number(process.env.RUN_HOURS ?? "2");
if (process.platform === "win32" || !Number.isFinite(hours) || hours <= 0 ||
    hours > 24 || process.argv.length > 4 || (process.argv[3] && !once)) {
    console.error("Usage on Linux: RUN_HOURS=<positive hours, at most 24> node run.js PROJECT [--once]");
    process.exit(2);
}

process.umask(0o077);
const state = fs.mkdtempSync(path.join(control, "run-"));
const deadline = Date.now() + hours * 3600000;
const heartbeat = path.join(control, "heartbeat.json");
const prompt = fs.readFileSync(path.join(control, "ITERATION.md"));
let attempt = 0;
let published = null;
let health = null;
let interrupted;
let phase = "starting";
let result = null;
process.on("SIGINT", () => { interrupted = "signal"; });
process.on("SIGTERM", () => { interrupted = "signal"; });

function stopReason() {
    return interrupted || (fs.existsSync(path.join(control, "STOP")) && "STOP") ||
        (Date.now() >= deadline && "deadline");
}

function beat() {
    const temporary = path.join(state, "heartbeat.tmp");
    fs.writeFileSync(temporary, JSON.stringify({
        phase, result, attempt, published, health, deadline: new Date(deadline).toISOString(),
        updated: new Date().toISOString(), logs: path.basename(state),
    }, null, 2) + "\n");
    fs.renameSync(temporary, heartbeat);
}

async function run(command, args, { input, limit = 60000, log } = {}) {
    const started = Date.now();
    if (stopReason()) return { code: null, output: "" };
    const errorFile = fs.openSync(path.join(state, log || "commands.log"), "a");
    try {
        const child = spawn(command, args, {
            cwd: repo, detached: true,
            env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
            stdio: [input ? "pipe" : "ignore", log ? errorFile : "pipe", errorFile],
        });
        let output = "";
        child.stdout?.setEncoding("utf8").on("data", chunk => { output = (output + chunk).slice(-65536); });
        child.stdin?.on("error", () => {});
        child.stdin?.end(input);

        function signalGroup(signal) {
            try { process.kill(-child.pid, signal); }
            catch (error) { if (error.code !== "ESRCH") throw error; }
        }

        let terminateAt;
        const timer = setInterval(() => {
            beat();
            const now = Date.now();
            if (terminateAt && now >= terminateAt) signalGroup("SIGKILL");
            if (!terminateAt && (stopReason() || now - started >= limit)) {
                terminateAt = now + 2000;
                signalGroup("SIGTERM");
            }
        }, 1000);
        try {
            const code = await new Promise(resolve => {
                child.once("exit", () => signalGroup("SIGKILL"));
                child.once("close", resolve);
                child.once("error", error => {
                    fs.writeSync(errorFile, `${command}: ${error.code}\n`);
                });
            });
            return { code, output: output.trim() };
        } finally {
            clearInterval(timer);
            if (child.pid) signalGroup("SIGKILL");
        }
    } finally {
        fs.closeSync(errorFile);
    }
}

const git = (...args) => run("git", ["-c", "core.hooksPath=/dev/null", ...args]);
const ok = async (...args) => (await git(...args)).code === 0;
async function value(...args) {
    const { code, output } = await git(...args);
    return code === 0 ? output : null;
}

class GuardError extends Error {}

async function verifyCheckout(base, remote) {
    if (stopReason()) return;
    if (await value("symbolic-ref", "--short", "HEAD") !== remote.branch ||
        await value("rev-parse", "HEAD") !== base ||
        await value("remote", "get-url", "--all", "origin") !== remote.fetch ||
        await value("remote", "get-url", "--push", "--all", "origin") !== remote.push ||
        await value("status", "--porcelain", "--untracked-files=all", "--", ...ownerFiles) !== "") {
        if (!stopReason()) throw new GuardError("checkout_or_owner_files_changed");
    }
}

async function iterate(remote) {
    phase = "syncing";
    beat();
    const before = await value("rev-parse", "HEAD");
    await verifyCheckout(before, remote);
    if (!await ok("fetch", "origin", remote.ref) ||
        !await ok("merge", "--ff-only", "FETCH_HEAD")) return "sync_failed";
    const base = await value("rev-parse", "HEAD");
    if (!base || !await ok("ls-files", "--error-unmatch", "--", ...ownerFiles)) {
        if (stopReason()) return "interrupted";
        throw new GuardError("owner_files_missing");
    }

    phase = "building";
    beat();
    const log = `codex-${attempt}.jsonl`;
    const turn = await run("codex", ["exec", "--dangerously-bypass-approvals-and-sandbox",
        "-c", 'model_reasoning_effort="medium"', "--json", "-"],
    { input: prompt, limit: 30 * 60000, log });
    await verifyCheckout(base, remote);
    if (stopReason()) return "interrupted";
    if (turn.code !== 0) {
        const events = fs.readFileSync(path.join(state, log), "utf8").split("\n");
        const diagnostics = events.filter(line => /^\s*\{\s*"type"\s*:\s*"(error|turn.failed)"/.test(line));
        return diagnostics.some(line => /usage.limit|insufficient.quota|rate.limit|too many requests/i.test(line))
            ? "usage_limit" : "codex_failed";
    }

    phase = "checking";
    beat();
    const check = await run(path.join(control, "health-check"), [], { log: `health-${attempt}.log` });
    health = check.code === 0 ? "passed" : "failed";
    await verifyCheckout(base, remote);
    if (check.code !== 0) return "health_failed";
    if (!await ok("add", "-A")) return "stage_failed";
    await verifyCheckout(base, remote);
    const staged = await git("diff", "--cached", "--quiet");
    if (staged.code === 1 && !await ok("commit", "-m", "Build next mission step")) return "commit_failed";
    if (staged.code !== 0 && staged.code !== 1) return "stage_failed";
    const head = await value("rev-parse", "HEAD");
    if (!head || !await ok("push", "origin", `HEAD:${remote.ref}`)) return "push_failed";
    published = head;
    return head === base ? "no_changes" : "published";
}

async function main() {
    beat();
    if (!fs.statSync(path.join(control, "health-check")).isFile()) throw new GuardError("health_check_missing");
    const branch = await value("symbolic-ref", "--short", "HEAD");
    const fetch = await value("remote", "get-url", "--all", "origin");
    const push = await value("remote", "get-url", "--push", "--all", "origin");
    if (stopReason()) return;
    if (!branch || !fetch || !push || fetch.includes("\n") || push.includes("\n") ||
        await value("status", "--porcelain", "--untracked-files=all") !== "") {
        throw new GuardError("clean_branch_and_single_origin_required");
    }
    const remote = { branch, fetch, push, ref: `refs/heads/${branch}` };
    while (!stopReason()) {
        attempt++;
        result = await iterate(remote);
        if (once || stopReason()) break;
        phase = "waiting";
        const retryAt = Date.now() + (result === "usage_limit" ? 900 : 60) * 1000;
        while (!stopReason() && Date.now() < retryAt) {
            beat();
            await delay(1000);
        }
    }
}

main().catch(error => {
    result = error instanceof GuardError ? error.message : error.code || "supervisor_error";
    process.exitCode = 1;
}).finally(() => {
    const reason = stopReason();
    phase = "stopped";
    if (reason) result = reason;
    if (once && result !== "published" && result !== "no_changes") process.exitCode = 1;
    beat();
    console.log(`Factory stopped: ${result}, attempts=${attempt}`);
});
