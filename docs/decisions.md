# Decisions and why

[plan.md](plan.md) records what was decided; this page records why, and what was
turned down, so the same options aren't proposed again without new facts. If the
two disagree, plan.md wins. "Owner" means the decision is the owner's own;
agents can't change those without asking.

Add a decision here when it's made, with the date and who made it. To reverse
one, record the new decision and say what changed; don't delete the old reason.

## VM layer

**Lima on every host** (owner, 2026-11-21). One VM layer with one config format
everywhere: `vz` on macOS, QEMU/KVM on Linux, and a Windows driver (HCS) in
progress. It supports many distros through templates, is written in Go, and is
an established open-source project.

- *Multipass:* rejected (owner). Built around Ubuntu images, and the owner wants
  other distros as guests.
- *Incus as the main layer:* rejected (owner). Linux hosts only. Kept as a
  fallback driver for the GPU box if Lima can't pass the GPU through.
- *Proxmox:* not adopted; a candidate for the Linux GPU box, undecided. It
  replaces the host OS and runs only on x86. See [alternatives](alternatives.md#proxmox-ve).

### macOS guest isolation

**Use stock Lima and the two-NIC design for macOS guests** (2026-11-28,
decided by the lead's worker overnight under the owner's autonomy grant).
The earlier [spike](spikes/macos-guests.md) kept macOS guests experimental
because Lima's SSH forward needs its built-in NIC. Keep that NIC for inbound
SSH under Seatbelt and use the confined socket gateway for egress. The initial
profile pins macOS 15.6.1 so it can run the locally available Xcode 16.2.
Limit the extra host-side access to the packaged agent, the exact ISO tool
and metadata on the APFS Data mount point.

**Keep macOS guests on the IPSW install path** (2026-11-28, decided by the
lead's worker overnight under the owner's autonomy grant). A VZ machine
identifier reset leaves the source disk's account and key material in place.
Ship the [iOS Simulator smoke check](../smoke/ios/README.md) independently of
the [macOS base image work](spikes/macos-guests.md#base-images).

### Linux host confinement

**Use unprivileged launch-time confinement** (implementation agent,
2026-11-27, implementing the owner's request to bring Linux host work
forward). QEMU's default SLIRP backend opens host sockets, so the launcher
restricts that backend and independently denies QEMU connects and
datagrams. Landlock and seccomp isolate files and sockets without installing
a privileged firewall service or editing a Docker host's firewall tables.
The gateway keeps the same public-IPv4 admission policy as on macOS.

**Use a private Unix SSH forward for the relay** (implementation agent,
2026-11-27). Landlock blocks procfs descriptor inspection across the
separately confined QEMU and gateway processes. A second SLIRP forward and
`SO_PEERPIDFD` verify the actual QEMU peer without expanding procfs access.
Lima's existing loopback SSH forward remains available to its hostagent.

**Windows waits for Lima's driver** (owner, 2026-11-21) rather than using
Multipass or Hyper-V directly, to keep one VM layer.

**No WSL2, anywhere** (owner). WSL2's Linux side has the Windows drives mounted
and can remount them as root, which defeats the isolation homebox exists for.

**Strong isolation over GPU** (owner, 2026-11-21). Hyper-V's GPU sharing (GPU-P)
isn't supported for Linux guests, so a Windows host can't give an isolated Linux
VM the GPU. The GPU comes from a Linux host on a separate drive of the gaming
PC instead (phase 5).

## Browser sandboxes inside the Ubuntu guest

**Allow unprivileged user namespaces** (owner, 2026-11-26).

Chromium's Linux sandbox, which Electron inherits, runs each web page's
renderer in its own user namespace (no view of the user's files, processes or
network) and under a seccomp filter (a short list of allowed system calls), so
a compromised page stays contained. Since 23.10, Ubuntu lets only programs
with an AppArmor profile create user namespaces
(`kernel.apparmor_restrict_unprivileged_userns = 1`), and npm- or
Playwright-installed browsers have none, so they ran with `--no-sandbox`.
[Ubuntu's AppArmor restriction](https://documentation.ubuntu.com/security/security-features/privilege-restriction/apparmor/)
reduces the kernel attack surface available for local privilege escalation:
user namespaces give unprivileged code root inside the namespace, which has
exposed kernel bugs (for example in netfilter and overlayfs) that let an
ordinary user become real root.
That restriction adds little protection for the VM's development user, who
already has passwordless sudo and root-equivalent Docker group membership.
Allowing user namespaces lets Chromium and Electron isolate renderers when
agents browse untrusted sites. The hypervisor, host network rules and host
process confinement enforce the VM-to-Mac boundary. Standard
[Fedora](https://packages.fedoraproject.org/pkgs/podman/podman/),
[Debian](https://wiki.debian.org/LXC) and [Arch](https://wiki.archlinux.org/title/Security#Kernel_hardening)
kernels allow unprivileged user namespaces by default.

- **Per-path AppArmor profiles** would need to track versioned npm and
  Playwright binary paths.
- **A setuid `chrome-sandbox` helper** would require a root-owned setuid
  binary for each installation, which fits poorly with user-local npm installs.
- **`--no-sandbox`** would require a per-app workaround and leave renderers
  without Chromium's process sandbox.

New VMs get the setting from [`guest/ubuntu/base.sh`](../guest/ubuntu/base.sh);
existing VMs get it by re-running provisioning (`box provision NAME`, or
**Re-run provisioning** in the app). The GUI smoke tests check that the
renderer sandbox is active.

## The box command

**Our own `box` command** (owner). Lima can change, so box hides it behind a
driver interface. And box gives a better experience than raw `limactl`: several
VMs, SSH shortcuts, isolation set up for you.

**Written in Go** (owner, 2026-11-21). One static binary per OS with nothing to
install on the host but Lima and box; the language of Lima and Tailscale.

- *Node or Python:* rejected (owner). Each needs a runtime on every host.
- *Bash:* rejected. Not native on Windows, so everything would need a
  PowerShell copy.
- *Make:* rejected. A build tool, not a command-line interface.

**Clone smoke images from local stores** (implementation, 2026-11-26).
The change tier copies the saved image into its scratch store with APFS
copy-on-write when available. This avoids compressing and rehashing the raw
disk each time, while preserving the runner's fresh-provisioning fallback
if staging fails. Archive imports still verify the disk bytes. See
[Images and sleep](images-and-sleep.md).

## Guests

**Ubuntu LTS first, any distro possible** (owner). Guest profiles are folders of
POSIX `sh` scripts, so they also run on Alpine's shell.

**No systemd requirement** (owner). The init system is the profile's business,
so non-systemd distros like Alpine stay possible.

**Base images built locally from the scripts** (owner, 2026-11-22). The scripts
stay the only definition of a VM; the image is a cache. A published, prebuilt
image would mean hosting multi-gigabyte files, a build per distro and CPU type,
trust in a binary blob, and frequent rebuilds as the agents update. It's a
possible later step, after checking whether Claude Code may be redistributed.

**Commit a build recipe for every project image** (owner, 2026-11-26).
Developers can reproduce the environment on another Mac using the
[image recipes](../images/README.md) while archives stay out of Git and releases.

**Copy the disk from a stopped Lima instance** (implementation, 2026-11-26).
A VZ Linux disk is standalone. Box can save it using APFS copy-on-write or a
sparse copy, then create a normal Lima config for each new VM. `limactl clone`
also copies instance-level configuration. Building a normal instance keeps the
allocated ports, confinement and identity setup on the existing creation path.
Preserve the VZ EFI variables so the guest retains its configured boot
entries. Lima still generates a new VZ machine identifier, and box pins
the guest's new SSH host keys.
See [Lima 2.3 disk handling](https://github.com/lima-vm/lima/blob/v2.3.0/pkg/driverutil/disk.go).

## Access and networking

**SSH straight into the VM** (owner). The host takes no SSH logins for box
(except a possible later account that can only run box).

**Egress rules enforced on the host** (owner). Rules inside the VM would be
undone by root in the VM.

**A gateway of box's own** for the VM's internet traffic, rather than Lima's
networking. Settled by the spikes: see [phase 0](spikes/phase0-macos.md) and
[UDP and ICMP](spikes/udp-macos.md).

**Default-deny file reads and Mach lookup in the macOS host profiles**
(implementation, 2026-11-22). The allowlists preserve access to VM state and
macOS runtime dependencies. Lima gets exact Mach service names for
virtualization, cache directory lookup and account resolution. The network
daemon uses no Mach services. See [networking and isolation](architecture.md#networking-and-isolation-macos).

**LAN publishing is opt-in per host** (owner), so a laptop-only setup exposes
nothing on the network.

**Tailscale for outside access; nothing served to the public internet** (owner,
2026-11-21).

**No server of our own, no paid service** (owner). homebox must work with just
the host and open-source software.

**Images stay on their host** (owner, 2026-11-22). Moving one is a manual copy of
an exported file. localbox copies images between Macs through its own hosted
relay, which homebox won't run.

**Autostart from a boot-time job, through box** (owner, 2026-11-22). A login item
would need someone to log in after a restart. `limactl autostart` would start
the VM without box's isolation. Hosts using autostart don't use FileVault, which
holds the boot until someone types the password.

**Verify the SSH listener in the relay process** (worker P, implementation,
2026-11-22). Use the kernel's socket identity before and after dialing so a
replacement listener cannot receive relayed bytes. The macOS check uses
`proc_info` inside the existing Seatbelt profile, which denies forking and
executing a tool such as `lsof`. See
[networking and isolation](architecture.md#networking-and-isolation-macos).

## Observability and the desktop app

Design: [observability.md](observability.md).

**One core, two views** (owner, 2026-11-22). Status, health and logs are built
once in box's core; the CLI and the desktop app only display them, so both
always show the same thing.

**Desktop version and module** (2026-11-22, phase 1b spike worker). Pin Wails v3.1.0
because v3 reached stable before work started. Use a nested Go module to keep
the desktop build dependencies outside the root CLI module. The
[spike](spikes/wails-desktop.md) records the build and accessibility evidence.

**The host pulls status from the VM over SSH** (2026-11-22). A guest agent that
reports to the host would open the VM→host channel that isolation blocks, and
guest-reported data is untrusted anyway, so box only displays it.

**Other hosts' VMs: only through the laptop's SSH to each VM** (owner,
2026-11-22). The app checks each saved host and shows unreachable, not running
or host key changed as hints. It can't start or stop VMs on another host.

- *A status-only SSH login to the host:* rejected (owner). A second login to set
  up per laptop and host, and SSH turned on for the host itself, which adds
  attack surface.
- *A box service on the LAN, found by Bonjour:* rejected (owner). Needs its own
  pairing and sign-in, adds a network service to defend, and is the most work.

**A desktop app, not a page in the browser** (owner, 2026-11-22).

**Wails for the desktop app** (owner, 2026-11-22). Its Go backend calls box's
core directly, and it runs on macOS, Windows and Linux like the hosts will.

- *Tauri or Electron:* not chosen. The Go core would need a bundled sidecar
  process and a bridge to it.
- *SwiftUI:* not chosen. macOS only.

**The app's look: the approved mockups** (owner, 2026-11-22). Dark, Inter
with JetBrains Mono for code, one blue accent; five screens in
[design/desktop-app.md](design/desktop-app.md).

**React with TypeScript for the app's frontend** (owner, 2026-11-22). xterm.js
and Monaco, which the app needs for its terminal and editor, have mature React
wrappers and the most examples.

- *Svelte:* not chosen. Lighter, but thinner support for xterm.js and Monaco.
- *Plain TypeScript:* not chosen. More hand-written code for five screens and
  their state.

**Build the app from source; no signed builds** (owner, 2026-11-22). homebox
isn't sold, so there's no Apple developer account (a paid service). Apps built
on your own Mac open normally.

**Remote desktop: low latency and bandwidth first** (owner, 2026-11-22), over
image quality and extra features. One server in the VM, reached over SSH, with
one web client used by the browser and the app. The owner selected **KasmVNC 1.5.2** (2026-11-24), using the SHA-256-pinned
GitHub release package, a server-controlled rate of 60 fps and WebSockets
over SSH with UDP blocked. In the [display-server spike](spikes/remote-desktop.md),
this combination had the lowest scrolling bandwidth and near-zero connected
idle cost. Selkies had a faster median and used more CPU when idle.
TigerVNC/noVNC had a slightly faster p95 and used substantially more bandwidth
in motion. A selectable RFB server for macOS Screen Sharing is future work.

**Native guest viewer** (implementation, 2026-11-24). The app hosts the
desktop in a separate native WKWebView layered over the tab. It loads the
same proxy as the CLI and keeps the guest page outside the Wails view and
its bindings. See [the isolation verification](../SECURITY.md#macos-guest-viewer).

**Explicit desktop clipboard transfers** (owner, 2026-11-24). The app
offers **Paste to VM** and **Copy from VM** for one-shot plain-text transfers.
To keep clipboard access outside the guest viewer, the trusted app uses its
existing pinned SSH connection and the guest's X11 clipboard. KasmVNC
clipboard transfer stays disabled. See [clipboard controls](../SECURITY.md#macos-guest-viewer).

**Remote host inventory and refresh** (worker M, implementation, 2026-11-22).
Keep aliases in the macOS user application-support directory so they are
independent of box's host records. Refresh each alias separately every 15
seconds with backoff up to 2 minutes after failure, so a sleeping Mac uses
fewer connection attempts while reachable guests keep reporting.
See [desktop implementation decisions](observability.md#desktop-implementation-decisions).

## Local test tiers

On 2026-11-26 the owner approved on-demand change, batch and full test tiers
on whichever Mac is available. The change tier targets a short pre-merge
feedback loop. The larger tiers check fresh provisioning and deeper guest
behavior less frequently. GitHub CI remains informative. See
[Testing](../README.md#testing) for the runner and partial runs.

On 2026-11-26 the owner chose the normal homebox state directory as the
default source for the saved smoke image. The runner keeps its VM state
isolated and accepts overrides for the source store and image name.

## Outside projects

**devvm: ideas only** (owner, 2026-11-21). MIT-licensed, and the source of the
Seatbelt confinement and the planned credential proxy. Not a dependency: one
maintainer and only weeks old. It also lacked a GPU and was narrower than a
general-purpose VM.

**exeuntu: the reference package list** for the Ubuntu profile (Apache-2.0).

**Docker Sandboxes: out** (owner). Closed source and needs a Docker account.

**localbox: ideas only** (owner, 2026-11-22). Images, sleep and resume, and macOS
guests come from it. It has no license, depends on a hosted relay, offers no SSH
for the user and doesn't block the host network. See
[alternatives](alternatives.md#localbox).

## Not decided

- The repository's license (owner: on hold).
- Proxmox or Lima for the Linux GPU box.

## 2026-11-26 · Smoke-test container image cache

The owner approved a Mac-side cache after the containers smoke runs hit Docker
Hub's anonymous pull quota. The [smoke runner](../smoke/README.md#container-image-cache)
stores archives on the Mac and transfers them over pinned SSH initiated by the
Mac, keeping the cache outside guest reach.

## Linux saved VM disks (2026-11-27)

The Linux smoke implementation uses Lima 2.3.0's standalone qcow2 disk
directly. Explicit backend and guest architecture metadata keep boot checks
specific to the hypervisor and CPU. The existing VZ archive layout stays
readable. Import and restore reject qcow2 references to other files so a
saved disk needs only the documented host firmware and its own contents.

The Linux smoke implementation added an explicit image option to the batch
and full tiers so the saved artifact can be exercised end to end. Their
default still provisions a fresh guest. Linux native desktop checks use a
disposable X11 display for clipboard and UI fixtures. The macOS Settings
fixture is reported separately when the runner uses the Linux verifiers.

## 2026-11-27 · Ubuntu desktop build

The owner requested that the desktop app run on the Ubuntu host. The
implementation uses Wails' default GTK4 build and a dedicated ephemeral
WebKitGTK viewer inside the existing layout. This keeps the viewer on the
same platform toolkit as the application while preserving the display proxy
and pinned SSH path. [Linux viewer isolation](../SECURITY.md#linux-guest-viewer)
records the WebKitGTK clipboard and navigation limits.

## 2026-11-28 · Guest agent factory smoke run

Decided by the lead's worker overnight under the owner's autonomy grant.
The supervisor runs in the guest with a separate health check and owns the
commit and push. This gives it a chance to check the owner files and app
before publication. The smoke project uses a bare origin in the disposable
guest, so it needs no GitHub credentials.

For this one smoke run, transfer the existing Codex login over pinned SSH
into the disposable guest with mode 0600. Use a minimal guest configuration.
Remove the guest credential and delete the VM afterward. Do not save an
image of it. The [agent login design](agent-logins.md) covers the proposed
long-term flow.

## Gateway socket recovery

**Guide a stop/start after gateway daemon failure** (2026-11-28, decided by
the lead's worker overnight under the owner's autonomy grant). Lima 2.3.0
hands VZ and QEMU a connected descriptor at boot. A daemon restarted at the
same socket path cannot replace that connection. Keep the same fail-closed
network and surface the recovery commands. Live recovery remains open and
would require a change to how the drivers hold the connection.

## Profile base images

**Managed profile base cache** (2026-11-28, decided by the lead's worker overnight
under the owner's autonomy grant). The profile definition, build scripts and
credential checks determine cache freshness. Build the Ubuntu disk at 20 GiB
and let the clone grow to the requested size. Keep manual image names intact.
Refresh a managed disk with an atomic directory exchange after verification,
so a failed build leaves the previous image available.

## 2026-11-28 · Mac isolation review

Decided by the lead's worker overnight under the owner's autonomy grant.
Include loopback aliases in the shared host-interface denial policy. macOS can
assign other address ranges to loopback interfaces and the existing policy
already denies the standard loopback address ranges. Drop unsupported IPv4
protocols before stack processing while preserving fragmentation for supported
transports. A failed guest doctor check now makes the command fail even
when SSH returns zero.
The [Mac isolation review](security-review-2026-11-28.md) records the checks
and residual limits.

## 2026-11-28 · Linux isolation review

Decided by the lead's worker overnight under the owner's autonomy grant.
Use the recorded QEMU launch pidfd identity in doctor, as the SSH relay
already does. Verify QEMU's restricted SLIRP configuration and connected
gateway descriptor after launch as well as checking the descriptor's peer
before exec. Restrict direct exec of library-tree files to the required ELF
loader, and deny process tracing and cross-process memory access in each
Linux confinement profile. Keep the hostagent and gateway's pathname Unix
sockets for current features and record the Landlock limit in SECURITY.md.
See the [Linux isolation review](security-review-2026-11-28-linux.md).

## 2026-11-28 · Host resource admission and Mac isolation coverage

Decided by the lead's worker overnight under the owner's autonomy grant.
Require 10 GiB available on the homebox state volume before accepting new VM
creation, a stopped VM start, or an image save, import or copy. A fixed
headroom floor leaves room for host writes without treating sparse virtual
disk sizes as physical allocations. Report available bytes in status and
show the floor in doctor. This is an admission check, so ongoing writes
can still use the remaining space.

Cap the SSH relay at 128 concurrent connections per VM and 64 admissions
per second with a burst of 128. Bound ownership checks, dials and copy
workers without terminating established sessions solely because of another
client's arrival. Keep Lima plain mode and the static forwarding config.
Test that replacing a published service leaves the published host endpoint
working and that an unpublished listener remains unavailable on the host.

The Docker e2e check requires a successful public DNS reply from inside the
container. Try independent resolvers so a single timed-out public endpoint
does not prevent exercising the local isolation assertions.

## 2026-11-28 · macOS Screen Sharing permission setup

Decided by the lead's worker overnight under the owner's autonomy grant.
Keep macOS Screen Sharing pending until a supported setup grants the guest
agent screen capture and input permissions while the RFB listener stays on
loopback. The [scratch guest test](spikes/macos-guests.md#screen-sharing-over-pinned-ssh)
authenticated through the existing proxy and received a black framebuffer.
The built-in backend needs a permission setup that works on fresh guests.

## 2026-11-28 · Nested KVM and Android Emulator

Decided by the lead's worker overnight under the owner's autonomy grant.
Expose host virtualization CPU features only when requested in the VM's
creation options. Mask VMX and SVM on the next start of existing Linux
x86-64 VMs to make the default effective even on Lima versions that choose
the host CPU automatically. The guest administrator grants KVM device
access to the guest user.

Use an opt-in headless Android smoke area with the AOSP x86-64 image,
software graphics and an SDK-built fixture APK. Keep the SDK and license
acceptance out of default provisioning.
