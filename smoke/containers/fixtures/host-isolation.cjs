const assert = require("node:assert/strict");
const { execFile } = require("node:child_process");
const { once } = require("node:events");
const { createServer } = require("node:http");
const { networkInterfaces } = require("node:os");
const { promisify } = require("node:util");

const run = promisify(execFile);
// The fixed Lima VZ host alias, also used by box doctor.
const limaHostAlias = "192.168.5.2";

async function probe(bind, target) {
  const server = createServer((_request, response) => response.end("smoke-canary"));
  try {
    server.listen(0, bind);
    await once(server, "listening");
    const { port } = server.address();
    const response = await fetch(`http://${bind}:${port}/`, { signal: AbortSignal.timeout(3000) });
    assert.equal(await response.text(), "smoke-canary");

    const { stdout } = await run(process.env.BOX || "box", [
      "ssh", process.env.SMOKE_VM, "--",
      `timeout 20 docker run --rm alpine:3.22 sh -ec '`
      + `command -v nc >/dev/null; if nc -w 3 ${target} ${port} </dev/null >/dev/null 2>&1; then exit 1; fi; echo blocked'`,
    ], { timeout: 25000 });
    assert.equal(stdout.trim(), "blocked");
  } finally {
    server.close();
    server.closeAllConnections();
  }
}

async function main() {
  if (process.argv[2] === "loopback") {
    await probe("127.0.0.1", limaHostAlias);
    return;
  }
  const addresses = Object.values(networkInterfaces()).flat()
    .filter(({ family, internal }) => family === "IPv4" && !internal)
    .map(({ address }) => address);
  if (!addresses.length) {
    process.exitCode = 77;
    return;
  }
  for (const address of new Set(addresses)) {
    await probe(address, address);
  }
}

main().catch(() => {
  console.error("Docker reached the host canary or its positive control failed.");
  process.exitCode = 1;
});
