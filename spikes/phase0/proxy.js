// Loopback-only proxy for the spike. Resolves and dials public IPv4 addresses.
const dns = require('node:dns').promises;
const http = require('node:http');
const net = require('node:net');

const blocked = new net.BlockList();
for (const [network, prefix] of [
  ['0.0.0.0', 8], ['10.0.0.0', 8], ['100.64.0.0', 10],
  ['127.0.0.0', 8], ['169.254.0.0', 16], ['172.16.0.0', 12],
  ['192.0.0.0', 24], ['192.0.2.0', 24], ['192.88.99.0', 24],
  ['192.168.0.0', 16], ['198.18.0.0', 15], ['198.51.100.0', 24],
  ['203.0.113.0', 24], ['224.0.0.0', 3],
]) {
  blocked.addSubnet(network, prefix);
}
for (const addresses of Object.values(require('node:os').networkInterfaces())) {
  for (const { address, family } of addresses) {
    if (family === 'IPv4') blocked.addAddress(address);
  }
}

async function destination(hostname) {
  const answers = await dns.lookup(hostname, { family: 4, all: true });
  if (answers.some(({ address }) => blocked.check(address))) {
    throw new Error('Destination denied');
  }
  return answers[0].address;
}

const server = http.createServer(async (request, response) => {
  try {
    const url = new URL(request.url);
    if (url.protocol !== 'http:') throw new Error('Only HTTP forwarding is supported');
    const upstream = http.request({
      host: await destination(url.hostname),
      port: url.port || 80,
      path: url.pathname + url.search,
      method: request.method,
      headers: { host: url.host },
      agent: false,
      timeout: 15000,
    }, (reply) => {
      response.writeHead(reply.statusCode, reply.headers);
      reply.pipe(response);
    });
    upstream.on('timeout', () => upstream.destroy());
    upstream.on('error', () => response.destroy());
    request.pipe(upstream);
  } catch {
    response.writeHead(403).end();
  }
});

server.on('connect', async (request, client, head) => {
  client.on('error', () => client.destroy());
  try {
    const [, hostname, port] = /^([^:\s/]+):(\d+)$/.exec(request.url) || [];
    if (!hostname || Number(port) < 1 || Number(port) > 65535) throw new Error('Invalid authority');
    const upstream = net.createConnection({ host: await destination(hostname), port: Number(port) });
    const timer = setTimeout(() => upstream.destroy(new Error('Connect timed out')), 10000);
    upstream.once('connect', () => {
      clearTimeout(timer);
      client.write('HTTP/1.1 200 Connection Established\r\n\r\n');
      upstream.write(head);
      client.pipe(upstream).pipe(client);
    });
    upstream.once('error', () => client.destroy());
    upstream.once('close', () => clearTimeout(timer));
    client.once('close', () => upstream.destroy());
  } catch {
    client.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n');
  }
});

server.listen(23128, '127.0.0.1');
