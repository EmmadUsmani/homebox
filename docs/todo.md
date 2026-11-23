# To do

Work agreed in principle but not built yet, roughly in priority order. Each item
says why it matters and the approach we lean towards; a spike settles the
details. Anything that changes the design document waits for the owner.

## Observability and the desktop app (phase 1b, next)

Agreed by the owner on 2026-11-22; design in [observability.md](observability.md).
In order (step 1 is done):

Step 1, the CLI (`box status`, `box top`, `box logs`, a richer `box ls` and
`--json`), was built on 2026-11-22. Left:

2. Desktop app with Wails: local VMs, remote VMs through the laptop's SSH
   (unreachable, not running and host key changed shown as hints), an SSH
   terminal, a file browser and a light Monaco editor. Keep its web-view
   dependencies out of the `box` binary. The look is approved
   ([design/desktop-app.md](design/desktop-app.md)); React with TypeScript.
3. Remote desktop, with phase 2's desktop in the VM: a spike picks the server
   (KasmVNC favoured, against TigerVNC with noVNC and Selkies) for latency and
   bandwidth, then `box desktop NAME` and the app's desktop tab.

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

## Resize a VM

CPUs, memory and disk are set once by `box new` (CPU and memory defaults scale
with the host, and disk defaults to 50 GiB)
and can't be changed afterwards; today the only way is `box rm` and `box new`,
which loses the VM's contents. Add `box resize NAME [--cpus N] [--memory GiB]
[--disk GiB]`:

- Works only on a stopped VM; update box's record and the driver's config
  together so they never disagree.
- Disk can only grow; refuse a smaller size. Grow the guest's filesystem on the
  next boot.
- Refuse more CPUs or memory than the host has, with a clear message, instead
  of leaving the VM unable to start.

## Sign agents in once per host

Every new VM starts with Claude Code and Codex signed out, so each `box new`
means two browser logins. The owner wants to sign in once per host. Approach:
`box auth add` stores a login on the host under box's state, outside every
sandbox's readable paths, and `box new` copies it into the VM. Prefer a
long-lived token made for machines without a browser. Copying a browser login
risks VMs signing each other out if refresh tokens rotate. A spike checks how
both agents store and refresh logins first. Design and open questions:
[agent-logins.md](agent-logins.md).

## Smaller

- Lima's SSH port forwarder leaves a connection in CLOSE_WAIT when the client
  side closes and never closes the guest side (seen with Lima 2.3.0). The
  guest's sshd keepalives clean these up within about a minute; report it
  upstream with a reproduction.
- Reconnect the socket NIC after a daemon failure without restarting the VM.
- Pass the gateway's MAC address and subnet to the guest from box instead of
  repeating them in `guest/ubuntu/network.sh`.
- Run the Linux process watcher (`internal/vmnet/host_process_linux.go`) on a
  real Linux host; so far it is only cross-compiled and vetted.
- NAT reflection: if the router reflects connections to its own public address
  into the LAN, the VM could reach a forwarded LAN port. Accepted; box could
  also deny the host's public address if it learned it without an outside
  service.

## Start VMs again when the host boots

The owner wants VMs back after a restart or power cut without logging in to the
host (design: "Autostart" in [plan.md](plan.md); a later phase, not urgent).
Approach: a mark in each box record, set by `box start` and cleared by `box
stop`, and `box autostart on`, which installs a boot-time job (launchd on macOS)
that runs box as the owner and starts every marked VM through box. Don't use
`limactl autostart`: it skips box's isolation. FileVault hosts aren't supported
(owner). The spike checks that a VM starts from a boot-time job with nobody
logged in; automatic login is the fallback.

## Add uv to the guest (low priority)

The owner agreed on 2026-11-22 to add Astral's uv, now the usual Python package and
project tool; exeuntu ships it too. Install it through mise in
`guest/common/mise.sh` (`mise use --global uv`), so it works the same on every
distro, and list it under "What's in the VM" in the README.
