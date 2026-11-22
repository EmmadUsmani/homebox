const net = require('node:net');

net.createServer((client) => {
  const ssh = net.createConnection(2201, '127.0.0.1');
  client.on('error', () => ssh.destroy());
  ssh.on('error', () => client.destroy());
  client.pipe(ssh).pipe(client);
}).listen(2202, '0.0.0.0');
