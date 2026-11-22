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

The relay now forwards only while the VM's host process runs, and laptops pin
each VM's host keys, so an impostor on the driver's loopback port fails the
host-key check. Left to do:

- Verify that the live host process owns the driver SSH listener before
  forwarding.

## Smaller

- Reconnect the socket NIC after a daemon failure without restarting the VM.
- Pass the gateway's MAC address and subnet to the guest from box instead of
  repeating them in `guest/ubuntu/network.sh`.
- Run the Linux process watcher (`internal/vmnet/host_process_linux.go`) on a
  real Linux host; so far it is only cross-compiled and vetted.
- NAT reflection: if the router reflects connections to its own public address
  into the LAN, the VM could reach a forwarded LAN port. Accepted; box could
  also deny the host's public address if it learned it without an outside
  service.
