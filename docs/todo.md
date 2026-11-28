# To do

Changes to the [approved design](plan.md) require the owner's approval.

## Desktop and remote access

- Repeat the desktop smoke from a second physical machine over the LAN.
  The existing [Linux desktop smoke checks](host-setup-linux.md#install-desktop-build-dependencies)
  use a pinned loopback alias for the VM-backed remote SSH path.
- Measure remote desktop latency, bandwidth and reconnect behavior over LAN
  and Tailscale. Test SSH from outside the LAN via Tailscale. See the
  [remote-desktop spike](spikes/remote-desktop.md).
- Establish a supported way to grant screen capture and input permissions
  before offering macOS Screen Sharing as a per-VM alternative to KasmVNC. The
  [loopback RFB prototype](spikes/macos-guests.md#screen-sharing-over-pinned-ssh)
  authenticated and reconnected but returned a black framebuffer.
- Exercise pinned SSH to the macOS guest from a separate laptop on the LAN.
  Local lifecycle checks use a separate laptop key and exported pins over loopback.
- Verify a [macOS image identity reset](spikes/macos-guests.md#base-images)
  before enabling macOS profile base images. An installer checkpoint needs
  a publication path that can prove the disk and Mac boot artifacts were
  captured before guest credentials existed.
- Review the [local lifecycle controls](design/desktop-app.md#local-lifecycle-controls-pending-owner-sign-off).
  The [parity audit](desktop-parity.md) tracks remaining desktop coverage,
  including a combined live resource view.

## Host and guest capabilities

- Prototype confined VFIO and verify CUDA and repeated GPU resets in an
  owner-attended Linux session. The [GPU passthrough spike](spikes/gpu-passthrough.md)
  records the host inventory and manual steps.
- Keep Windows host support on the [design roadmap](plan.md#phases).
- Check [autostart on Ubuntu](host-setup-linux.md#start-vms-at-host-boot)
  after an owner-attended reboot, before login.
- Check [autostart on macOS](host-setup-macos.md#start-vms-at-host-boot)
  under launchd before login. If Virtualization.framework needs a user session,
  test the LaunchAgent with automatic login. FileVault hosts remain outside
  the design.
- Track aggregate host storage quotas. The current
  [10 GiB admission floor](architecture.md#host-state-volume-headroom) checks
  free space before selected operations. Aggregate allocation enforcement
  remains open.
- Spike [sleep and resume](images-and-sleep.md) with live process state.
- Evaluate a dedicated Android guest profile. The existing
  [Android smoke area](../smoke/android/README.md) runs a headless AOSP emulator
  inside Ubuntu with opt-in nested KVM.
- Verify the factory's optional [systemd user unit](../factory/README.md)
  on an Ubuntu guest. The Linux factory run verified the supervisor.

## Isolation and recovery

- Apply the [browser sandbox setting](../SECURITY.md#browser-processes-inside-the-ubuntu-guest)
  to existing Ubuntu VMs such as `dev` with `box provision NAME` or
  **Re-run provisioning** in the app. New Ubuntu VMs receive it during provisioning.

- Check the Files tab with unusual filenames in the macOS GUI. See the
  [Mac isolation review](security-review-2026-11-28.md).
- Run macOS VMs as a separate user so Unix permissions add another boundary.
- Extend Linux confinement as newer Landlock pathname Unix-socket controls
  become available. Recheck the QEMU launch contract when updating Lima.
  See the [Linux isolation review](security-review-2026-11-28-linux.md).
- Investigate live socket NIC recovery. Today a stop/start restores egress
  after daemon failure. See the [gateway recovery decision](decisions.md#gateway-socket-recovery).
- Report Lima 2.3.0's SSH forwarder CLOSE_WAIT behavior upstream with a
  reproduction. Guest sshd keepalives clean up the connection within about
  a minute.
- **Accepted risk.** With NAT reflection enabled, a guest can reach a LAN
  port forward through the router's public address. Box could deny that
  address if it can learn it without an outside service. See the
  [gateway limitations](architecture.md#networking-and-isolation-macos).

## Agent sign-in (deferred)

The owner deferred host-assisted sign-in on 2026-11-22. The longer-term
credential proxy is still on the [design roadmap](plan.md#phases). The
[agent login design](agent-logins.md) and [spike](spikes/agent-logins.md)
record the proposal awaiting the owner: a host-stored Claude setup token
copied into each new VM and a per-VM Codex device-code login.

## Verified milestones

- **Host and desktop support.** The CLI and Wails desktop app run on macOS and
  supported x86-64 Ubuntu. The Ubuntu remote desktop runs over pinned SSH
  and supports one-shot plain-text clipboard transfers. See
  [desktop observability](observability.md) and the
  [native verifier results](test-runs.md).
- **Ubuntu images and LAN access.** Both hosts support saved disks and
  automatic base images with `box image rebuild`. See
  [the Ubuntu base recipe](../images/base-ubuntu/README.md), the
  [smoke image recipe](../images/homebox-smoke/README.md),
  [Linux image verification](images-and-sleep.md#linux-verification) and
  [Linux LAN publishing measurements](host-setup-linux.md#lan-publishing-verification).
- **macOS guest and iOS Simulator.** The Sequoia profile boots with pinned SSH
  and the homebox gateway. The [iOS smoke area](../smoke/ios/README.md)
  automates the Simulator boot and sample app.
- **Host confinement.** The isolation reviews above record the Mac and Linux
  hardening and coverage, including SSH relay ownership and gateway fixtures.
- **Guest factory.** The [verified factory runs](../factory/README.md#verified-guest-runs)
  completed one turn on a Mac-hosted guest and ten turns on a Linux-hosted guest.
- **Repeatable smoke tests.** The [smoke runner](../smoke/README.md#container-image-cache)
  caches container images on the host. Cold cache fills still depend on Docker
  Hub availability and anonymous pull quotas. The [change tier](../README.md#testing)
  uses the normal homebox image store by default and also supports an alternate
  store or archive. Its history records the source and disk digest. The VM
  lifecycle test verifies Docker UDP DNS via multiple public resolvers.
- **Guest tooling.** Ubuntu installs [uv through mise](../guest/common/mise.sh).
