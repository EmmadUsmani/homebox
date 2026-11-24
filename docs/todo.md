# To do

Work agreed in principle but not built yet, roughly in priority order. Each item
says why it matters and the approach we lean towards; a spike settles the
details. Anything that changes the design document waits for the owner.

## Observability and the desktop app (phase 1b)

Agreed by the owner on 2026-11-22; design in [observability.md](observability.md).
Steps 1 and 2 were built on 2026-11-22: the CLI (`box status`, `box top`,
`box logs`, a richer `box ls`, `--json`) and the macOS desktop app (local and
remote VMs, overview, logs, terminal, files and editor).

3. Remote desktop, with phase 2's desktop in the VM. The
   [display-server spike](spikes/remote-desktop.md) led to the owner's
   selection of KasmVNC. Both `box desktop NAME` and the app's Desktop tab
   are built and verified on a local Mac and VM.

- Measure end-to-end desktop latency, bandwidth and reconnect behavior over
  LAN and Tailscale. See [the remote-desktop spike](spikes/remote-desktop.md).

- Optional second display server for macOS Screen Sharing (RFB), selectable per VM alongside KasmVNC.

## Tighten the host sandboxes further

Both host processes now allow-list file reads and Mach service lookups. See
[networking and isolation](architecture.md#networking-and-isolation-macos).
Left to do:

- Run the VMs as a separate macOS user, so Unix permissions protect the
  owner's files even if a sandbox has a gap.

## Harden the SSH relay

Done. The relay verifies that the live host process owns the driver's SSH
listener before dialing and checks that the same listener still belongs to
that process before forwarding. `box doctor` records the ownership check.

## Sign agents in once per host (deferred)

Deferred by the owner on 2026-11-22 while other work goes first. The spike is
done and a proposal waits for the owner: a Claude `setup-token` stored on the
host and copied in by `box new`, and a per-VM Codex device-code login. See
[agent-logins.md](agent-logins.md) and the
[spike](spikes/agent-logins.md).

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
