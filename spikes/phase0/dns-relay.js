const dgram = require('node:dgram');
const listener = dgram.createSocket('udp4');
listener.on('message', (query, client) => {
  const upstream = dgram.createSocket('udp4');
  const timer = setTimeout(() => upstream.close(), 2000);
  upstream.on('close', () => clearTimeout(timer));
  upstream.on('error', () => upstream.close());
  upstream.once('message', (reply) => {
    listener.send(reply, client.port, client.address);
    upstream.close();
  });
  upstream.connect(53, '1.1.1.1', () => upstream.send(query));
});
listener.bind(23553, '127.0.0.1');
