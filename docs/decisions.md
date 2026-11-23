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

**Windows waits for Lima's driver** (owner, 2026-11-21) rather than using
Multipass or Hyper-V directly, to keep one VM layer.

**No WSL2, anywhere** (owner). WSL2's Linux side has the Windows drives mounted
and can remount them as root, which defeats the isolation homebox exists for.

**Strong isolation over GPU** (owner, 2026-11-21). Hyper-V's GPU sharing (GPU-P)
isn't supported for Linux guests, so a Windows host can't give an isolated Linux
VM the GPU. The GPU comes from a Linux host on a separate drive of the gaming
PC instead (phase 5).

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

## Access and networking

**SSH straight into the VM** (owner). The host takes no SSH logins for box
(except a possible later account that can only run box).

**Egress rules enforced on the host** (owner). Rules inside the VM would be
undone by root in the VM.

**A gateway of box's own** for the VM's internet traffic, rather than Lima's
networking. Settled by the spikes: see [phase 0](spikes/phase0-macos.md) and
[UDP and ICMP](spikes/udp-macos.md).

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
one web client used by the browser and the app. The owner initially favoured
KasmVNC. The [display-server spike](spikes/remote-desktop.md) is complete and
server selection awaits the owner. macOS Screen Sharing is nice to have (owner).

**Remote host inventory and refresh** (worker M, implementation, 2026-11-22).
Keep aliases in the macOS user application-support directory so they are
independent of box's host records. Refresh each alias separately every 15
seconds with backoff up to 2 minutes after failure, so a sleeping Mac uses
fewer connection attempts while reachable guests keep reporting.
See [desktop implementation decisions](observability.md#desktop-implementation-decisions).

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
