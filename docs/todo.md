# To do

Work agreed in principle but not built yet, roughly in priority order. Each item
says why it matters and the approach we lean towards; a spike settles the
details. Anything that changes the design document waits for the owner.

## Tighten the host sandboxes further

Both host processes now have file and process limits (see "macOS networking"
in the architecture). Left to do:

- Allow-list system services (`mach-lookup`) instead of denying a few by
  name, and limit reads outside the home directory (`/Volumes`,
  `/Users/Shared`, other apps' temp files).
- Run the VMs as a separate macOS user, so Unix permissions protect the
  owner's files even if a sandbox has a gap.

## Harden the SSH relay

The relay forwards the published SSH port to the hostagent's loopback port. While
the VM is down that port is free, and any program on the Mac could listen there
and receive the laptop's SSH connection. The laptop's host-key check catches it
once the laptop knows the VM's key; a first connection would trust it.

- Pin host keys on the laptop: `box ssh-config` also prints the VM's host keys,
  so there is no first-use trust (this also defeats a LAN man-in-the-middle).
- Verify that the live host process owns the driver SSH listener before forwarding.

## Using the VM on the host Mac itself

A Must requirement since 2026-11-22 (design document). Today `box ssh NAME`
works on the host, and the published ports work on `127.0.0.1`. Missing pieces:

- `box ssh-config --local` (or an `Include` file box maintains), so plain
  `ssh main`, `scp`, `rsync` and editors' remote-SSH modes work on the host
  without authorizing another key.
- `box open NAME PORT` (or memorable local names) for dev servers.

## Smaller

- Reconnect the socket NIC after daemon failure without restarting the VM.
- Run the CI workflow on GitHub; the Linux tests have only been cross-compiled
  and vetted locally.
- NAT reflection: if the router reflects connections to its own public address
  into the LAN, the VM could reach a forwarded LAN port. Accepted; box could
  also deny the host's public address if it learned it without an outside
  service.
