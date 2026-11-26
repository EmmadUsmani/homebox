const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");

function assertSandboxedRenderer(pid) {
    const status = readFileSync(`/proc/${pid}/status`, "utf8");
    assert.match(status, /^Seccomp:[ \t]+2$/m, `Renderer ${pid} must use seccomp filtering`);
    assert.match(status, /^NSpid:[ \t]+\d+(?:[ \t]+\d+)+$/m,
        `Renderer ${pid} must run in a nested PID namespace`);
}

module.exports = assertSandboxedRenderer;
