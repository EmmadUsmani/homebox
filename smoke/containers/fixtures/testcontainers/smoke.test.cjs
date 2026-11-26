const assert = require("node:assert/strict");
const test = require("node:test");
const { GenericContainer, Wait } = require("testcontainers");

test("a fresh HTTP container responds over its mapped port", async () => {
  const container = await new GenericContainer("nginx:1.28-alpine")
    .withExposedPorts(80)
    .withWaitStrategy(Wait.forHttp("/", 80))
    .withStartupTimeout(60_000)
    .start();
  try {
    const response = await fetch(
      `http://${container.getHost()}:${container.getMappedPort(80)}/`,
      { signal: AbortSignal.timeout(5000) },
    );
    assert.equal(response.status, 200);
    assert.match(await response.text(), /Welcome to nginx/);
  } finally {
    await container.stop();
  }
});
