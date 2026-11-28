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

   The app also supports [one-shot plain-text clipboard transfers](observability.md#remote-desktop-phase-2).

- Extend the Ubuntu [desktop smoke checks](host-setup-linux.md#install-desktop-build-dependencies)
  to a second physical host over the LAN. The Linux VM-backed smoke uses a
  pinned loopback alias for the remote SSH path. Full creation, provisioning,
  and deletion through the Linux GUI still need a dedicated run.

- The local Settings tab and About close the host settings and version gaps.
  The [parity audit](desktop-parity.md) tracks remaining coverage, including a
  combined live resource view.

- Measure end-to-end desktop latency, bandwidth and reconnect behavior over
  LAN and Tailscale. See [the remote-desktop spike](spikes/remote-desktop.md).

- Optional second display server for macOS Screen Sharing (RFB), selectable per VM alongside KasmVNC.

- Review the desktop app's [local lifecycle controls](design/desktop-app.md#local-lifecycle-controls-pending-owner-sign-off).

## Linux host follow-ups

The core CLI work uses Lima 2.3.0, QEMU/KVM and the shared IPv4 gateway.
The [Linux architecture notes](architecture.md#networking-and-isolation-linux)
cover the sandbox and its limits. Linux saved disks and the full smoke
runner image are tracked separately from the core lifecycle. Measure LAN
publishing from a second machine and cover GPU and autostart separately.

## macOS guests and the iOS Simulator

The [macOS guest spike](spikes/macos-guests.md) covers Lima's installer, first
boot and socket NIC. Before adding a `box` profile, establish a pinned SSH path
with the homebox gateway as the only egress path and validate host confinement
and `box doctor` on a fresh guest. Test the iOS Simulator once Xcode and a
runtime can be staged into that guest.

## Update existing Ubuntu VMs for browser sandboxing

New VMs get the [browser sandbox setting](../SECURITY.md#browser-processes-inside-the-ubuntu-guest)
during provisioning. Apply it to existing VMs such as `dev` with
`box provision NAME` or **Re-run provisioning** in the app.

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
- Live socket NIC recovery remains open. The current Lima attachments require
  a VM restart after a daemon failure. Box detects this and reports the
  recovery commands. See [the gateway recovery decision](decisions.md#gateway-socket-recovery).
- Extend Linux sandbox coverage as newer Landlock pathname Unix-socket
  controls become available. Recheck the QEMU launch contract when updating Lima.
- NAT reflection: if the router reflects connections to its own public address
  into the LAN, the VM could reach a forwarded LAN port. Accepted; box could
  also deny the host's public address if it learned it without an outside
  service.

## Base images (phase 2)

The Mac VZ backend supports automatic profile bases and manual refresh with
`box image rebuild [--profile P]`. See the [Ubuntu recipe](../images/base-ubuntu/README.md).
Linux base images depend on Linux saved-disk support.

## Start VMs again when the host boots

The owner wants VMs back after a restart or power cut without logging in to the
host (design: "Autostart" in [plan.md](plan.md); a later phase, not urgent).
Approach: a mark in each box record, set by `box start` and cleared by `box
stop`, and `box autostart on`, which installs a boot-time job (launchd on macOS)
that runs box as the owner and starts every marked VM through box. Don't use
`limactl autostart`: it skips box's isolation. FileVault hosts aren't supported
(owner). The spike checks that a VM starts from a boot-time job with nobody
logged in; automatic login is the fallback.

## Add uv to the guest (done)

The owner agreed on 2026-11-22 to add Astral's Python project tool. The guest
installs [uv through mise](../guest/common/mise.sh).

## Docker Hub pull limits in smoke runs

Done on 2026-11-26. The [smoke runner](../smoke/README.md#container-image-cache)
caches image archives on the Mac to avoid the Docker Hub anonymous pull limit
(HTTP 429) on repeat runs. Cold fills still depend on registry availability
and pull quotas.

## Smoke image selection for the change tier

Done on 2026-11-26. The [change tier](../README.md#testing) takes its saved
image from the normal homebox store by default and can use another store
or import an archive directly. The run history records the source and disk digest.

## Agent factory

Run the [guest factory smoke](../factory/README.md#one-turn-smoke-test) on
the remote Ubuntu host and verify the user unit there.
