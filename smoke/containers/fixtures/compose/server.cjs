const { execFile } = require("node:child_process");
const { createServer } = require("node:http");
const { promisify } = require("node:util");

const run = promisify(execFile);
createServer(async (_request, response) => {
  try {
    const [postgres, redis] = await Promise.all([
      run("psql", ["-h", "postgres", "-U", "postgres", "-d", "smoke", "-Atc", "SELECT 42"], { timeout: 2000 }),
      run("redis-cli", ["-h", "redis", "--raw", "PING"], { timeout: 2000 }),
    ]);
    if (postgres.stdout.trim() !== "42" || redis.stdout.trim() !== "PONG") {
      throw new Error("Unexpected database response");
    }
    response.end("compose:42:PONG");
  } catch (error) {
    response.writeHead(503).end(error.message);
  }
}).listen(8080, "0.0.0.0");
