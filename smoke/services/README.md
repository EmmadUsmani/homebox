# Service and network smoke tests

Quick uses Postgres as the representative database and Redis as the
representative cache. It also covers RabbitMQ, Garage S3, HTTP/TLS,
WebSocket, SSE, GraphQL, gRPC, local WebRTC, DNS A lookup, public HTTPS,
and the Mac’s published-port and isolation checks.

Full adds SQLite, MariaDB, Valkey, Memcached and MongoDB, larger transfers,
public STUN and more DNS record types and HTTPS destinations.

Run this area against a disposable Ubuntu homebox VM with two CPUs and 4 GiB.

```sh
smoke/run.sh smoke-services services
```

Setup installs PostgreSQL, MariaDB, Redis, Valkey, memcached, RabbitMQ,
nginx, Caddy and mkcert from Ubuntu. It installs Python client libraries
in a virtual environment. The [smoke runner](../README.md#container-image-cache)
stages versioned Linux ARM64-capable Garage and MongoDB images. Allow several
minutes for setup and about two minutes for healthy checks.
[Garage quick start](https://garagehq.deuxfleurs.fr/documentation/quick-start/)

- Python clients assert writes and reads across process restarts, including
  SQLite WAL, FTS5 and JSON, PostgreSQL extensions, Redis and Valkey
  snapshots, streams and pub/sub, an acknowledged durable RabbitMQ
  message, and Garage and MongoDB data. Memcached is expected to lose its
  cached value on restart. SQLite closes and reopens in separate processes.
- nginx and Caddy proxy real requests. Checks cover HTTP/1.1, HTTP/2 with a
  verified mkcert root, multiple WebSocket messages, server-sent events,
  16 MiB uploads and downloads, gRPC and GraphQL. The fixture CA stays in a
  temporary directory. Chromium inside the guest exchanges WebRTC data and
  video frames. Chromium also gathered a server-reflexive candidate from
  Google’s public STUN server through the IPv4 UDP gateway on the local Mac.
- Outbound checks exercise Git over HTTPS, public package registries and
  DNS record types. IPv6 transport is expected to fail under the documented
  gateway policy, even though AAAA lookups work.
- On the Mac, the runner uses Node.js 22 or later for the WebSocket client.
  It connects to both published fixtures, checks actual listener bindings
  in loopback or LAN mode, and delegates the host and private-network
  negative checks to `box doctor`. Run it with the default guest mappings
  for ports 8000 and 8001.

The checks restart packaged services and reserve fixture database names and
ports, so use a scratch VM. Test containers, certificates, data and proxy
processes are cleaned up. Setup packages and downloaded images stay in the
VM for repeat runs.
