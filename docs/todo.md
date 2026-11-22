# To do

Work agreed in principle but not built yet, roughly in priority order. Each item
says why it matters and the approach we lean towards; a spike settles the
details. Anything that changes the design document waits for the owner.

## UDP and ICMP from the VM

Today only TCP (through the proxy) and DNS leave the VM. QUIC/HTTP3 falls back
to TCP, but WebRTC, NTP, mosh, Tailscale's direct paths, game and media servers,
other DNS resolvers and `ping` don't work. A full development machine should
have them.

Seatbelt can't filter by address, so UDP needs a host-side policy like the TCP
proxy. Options:

1. **box owns the VM's network (preferred).** Give the VM a second network
   interface on a unix socket that box's daemon serves (Lima's `networks:
   - socket:`), and make it the default route. The daemon runs a userspace
   TCP/IP stack (gVisor's netstack, as Lima's own user network does) and applies
   the public-addresses-only policy per flow for TCP, UDP and ICMP (macOS allows
   unprivileged ICMP datagram sockets). This replaces the guest's redsocks
   redirect, and the guest can't bypass it: there is no other way out. Lima's
   own interface stays confined by Seatbelt for SSH. Spike first: does `vz`
   accept a socket network next to the built-in one, and how fast is it?
2. **SOCKS5 UDP.** Add UDP ASSOCIATE to the daemon and a transparent UDP
   redirect in the guest. Smaller, but UDP only (no ICMP), and it adds another
   guest-side daemon.

## Confine the hostagent's file access

Seatbelt limits only the network of Lima's hostagent. If a hostile guest found a
bug in it, the hostagent could read the host user's files (`~/.ssh`, browser
data) and write them. A write to a LaunchAgent or a shell profile runs code
outside the sandbox later, so the network limit wouldn't hold either.

Seatbelt's file filters, unlike its network filters, can say exactly what we
need. Plan: deny writes outside the instance and cache directories, deny reads
of the home directory except Lima's own paths, limit `process-exec` to Lima's
binaries, and block LaunchServices. Then `box doctor` proves it by reading and
writing a canary file under the same profile. Also confine box's network daemon
(proxy, DNS, relay), which parses guest traffic and runs unconfined: it needs
outbound network but only its log file. A separate macOS user for the VMs is
the stronger, heavier follow-up.

## Harden the SSH relay

The relay forwards the published SSH port to the hostagent's loopback port. While
the VM is down that port is free, and any program on the Mac could listen there
and receive the laptop's SSH connection. The laptop's host-key check catches it
once the laptop knows the VM's key; a first connection would trust it.

- Pin host keys on the laptop: `box ssh-config` also prints the VM's host keys,
  so there is no first-use trust (this also defeats a LAN man-in-the-middle).
- Open the relay only while the hostagent runs, and check that the hostagent's
  process owns the listening socket before forwarding.

## Using the VM on the host Mac itself

A Must requirement since 2026-11-22 (design document). Today `box ssh NAME`
works on the host, and the published ports work on `127.0.0.1`. Missing pieces:

- `box ssh-config --local` (or an `Include` file box maintains), so plain
  `ssh main`, `scp`, `rsync` and editors' remote-SSH modes work on the host
  without authorizing another key.
- `box open NAME PORT` (or memorable local names) for dev servers.

## Smaller

- Pass the guest network values (gateway address, proxy and DNS ports) from the
  driver instead of hardcoding Lima's in the guest scripts.
- Run the CI workflow on GitHub; the Linux tests have only been cross-compiled
  and vetted locally.
- NAT reflection: if the router reflects connections to its own public address
  into the LAN, the VM could reach a forwarded LAN port. Accepted; box could
  also deny the host's public address if it learned it without an outside
  service.
