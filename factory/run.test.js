"use strict";

const assert = require("node:assert/strict");
const { execFileSync, spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

function fixture(t, turn, health = "true") {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "homebox-factory-"));
    t.after(() => fs.rmSync(root, { recursive: true, force: true }));
    const repo = path.join(root, "project");
    const origin = path.join(root, "origin.git");
    const control = path.join(root, "control");
    fs.mkdirSync(repo);
    fs.mkdirSync(control);
    const env = {
        ...process.env, HOME: root, GIT_CONFIG_NOSYSTEM: "1", RUN_HOURS: "0.05",
        PATH: `${control}:${process.env.PATH}`,
    };
    const git = (...args) => execFileSync("git", args, { cwd: repo, env, stdio: ["ignore", "pipe", "pipe"] }).toString().trim();
    git("init", "--bare", origin);
    git("init", "-b", "main");
    git("config", "user.name", "Factory Test");
    git("config", "user.email", "factory@example.invalid");
    for (const file of ["MISSION.md", "STEERING.md"]) fs.writeFileSync(path.join(repo, file), file);
    git("add", ".");
    git("commit", "-m", "Seed project");
    git("remote", "add", "origin", origin);
    git("push", "origin", "main");

    for (const file of ["run.js", "ITERATION.md"]) fs.copyFileSync(path.join(__dirname, file), path.join(control, file));
    fs.writeFileSync(path.join(control, "codex"), `#!/bin/sh\n${turn}\n`, { mode: 0o700 });
    fs.writeFileSync(path.join(control, "health-check"), `#!/bin/sh\n${health}\n`, { mode: 0o700 });
    const initial = git("rev-parse", "HEAD");
    return {
        initial, repo, control, git,
        run(overrides = {}) {
            const { status, error } = spawnSync(process.execPath, [path.join(control, "run.js"), repo, "--once"],
                { env: { ...env, ...overrides }, timeout: 10000 });
            assert.ifError(error);
            return { status, ...JSON.parse(fs.readFileSync(path.join(control, "heartbeat.json"), "utf8")) };
        },
        remoteHead: () => git("--git-dir", origin, "rev-parse", "main"),
    };
}

test("fast-forwards steering and publishes verified work", t => {
    const project = fixture(t, "grep -qx updated STEERING.md && echo done > app.txt", "test -s app.txt");
    const other = path.join(project.control, "other");
    project.git("clone", "-b", "main", project.git("remote", "get-url", "origin"), other);
    fs.writeFileSync(path.join(other, "STEERING.md"), "updated\n");
    project.git("-C", other, "config", "user.name", "Factory Test");
    project.git("-C", other, "config", "user.email", "factory@example.invalid");
    project.git("-C", other, "commit", "-am", "Update steering");
    project.git("-C", other, "push", "origin", "main");
    const run = project.run();
    assert.equal(run.status, 0);
    assert.equal(run.result, "published");
    assert.equal(run.published, project.remoteHead());
    assert.equal(project.git("show", "HEAD:app.txt"), "done");
});

test("staged owner edits block publication even if the working copy is restored", t => {
    const project = fixture(t, "echo changed > STEERING.md; git add STEERING.md; git show HEAD:STEERING.md > STEERING.md");
    const run = project.run();
    assert.equal(run.status, 1);
    assert.equal(run.result, "checkout_or_owner_files_changed");
    assert.equal(project.remoteHead(), project.initial);
});

test("failed health checks leave the candidate unpublished", t => {
    const project = fixture(t, "echo done > app.txt", "false");
    const run = project.run();
    assert.equal(run.status, 1);
    assert.equal(run.result, "health_failed");
    assert.equal(project.remoteHead(), project.initial);
    assert.equal(fs.readFileSync(path.join(project.repo, "app.txt"), "utf8"), "done\n");
});

test("the deadline terminates an active turn", t => {
    const project = fixture(t, "echo started > started; sleep 10; touch completed");
    const run = project.run({ RUN_HOURS: "0.0006" });
    assert.equal(run.result, "deadline");
    assert.ok(fs.existsSync(path.join(project.repo, "started")));
    assert.ok(!fs.existsSync(path.join(project.repo, "completed")));
    assert.equal(project.remoteHead(), project.initial);
});
