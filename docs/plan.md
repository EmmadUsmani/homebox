# homebox: design (v6, 2026-11-22)

Status: signed off by the owner on 2026-11-21. Phases 0 and 1 built on 2026-11-22.
v6 (owner, 2026-11-22) adds images, sleep and resume, and macOS guests, after
comparing [localbox](alternatives.md#localbox); restarting VMs when the host
boots; and base images, so new VMs start from an image instead of provisioning.

**This document changes only with the owner's explicit consent.** See
[AGENTS.md](../AGENTS.md).

In short: Lima is the only VM layer, used through our own `box` command so it
stays swappable; guests are pluggable profiles, with Ubuntu as the primary one; no
systemd requirement; the host tooling needs no runtime installed.

## Goal

SSH from my laptop into a full Linux VM running on my own machine (a Mac Mini
first, then my Windows PC), and use it the way I use exe.dev: run Claude Code and
Codex with full permissions, run any server, render and screenshot UI.

## Requirements

| Requirement | Priority |
|---|---|
| Hosts: macOS (Mac Mini, M1/M2 assumed), then Windows, then Linux (GPU box) | Must, Mac first |
| Full, general-purpose Linux VM: Docker, any language, servers | Must |
| SSH straight into the VM from another machine on my home network | Must |
| Use the VM from the host machine itself: ssh, editors' remote-SSH, dev servers in a local browser | Must |
| Agents render UI and take screenshots (headless browser, Xvfb for headed apps) | Must |
| Claude Code and Codex on my own accounts, full-permission mode | Must |
| Strong isolation: nothing in the VM reaches my host's files, accounts, keys or apps | Must, beats GPU |
| VM reaches the public internet (APIs, SaaS, search, browsing) | Must |
| VM does not reach my host or other home devices | Must |
| `box` command for provisioning and shortcuts; SSH for everything inside the VM | Must |
| VM layer swappable behind `box` (Lima now; maybe Incus on Linux later) | Must (architecture) |
| Guest distro pluggable; Ubuntu primary; no systemd requirement | Must (architecture); only Ubuntu shipped |
| Host tooling needs no language runtime on the host | Must |
| HTTP from my laptop to servers in the VM | Nice |
| Several VMs per host | Nice, don't design it out |
| Images: save a VM as an image and start new VMs from it | Nice, phase 2 |
| Images stay on the host that made them; moving one to another host is a manual copy | Constraint |
| Base images: `box new` starts from a local image built by the profile's scripts, instead of running them every time | Nice, phase 2 |
| A prebuilt base image published on GitHub Releases | Later, maybe |
| Sleep and resume: an idle VM pauses, then resumes with its processes still running | Later |
| VMs that were running start again, isolated as usual, when the host boots, before anyone logs in | Later |
| Autostart on hosts with FileVault (disk encryption locked until someone types the password) | Not supported (owner) |
| Remote access from outside my network via Tailscale (approved) | Later |
| Desktop + remote desktop | Later |
| GPU on a Linux host (gaming PC, separate drive, RTX 3080 Ti) | Later, design for it |
| Android; macOS guests (for iOS and Mac apps); Windows guests | Later, keep possible |
| No server of my own, no paid service, no WSL2 | Constraint |
| Nothing served to the public internet; outside access only through Tailscale | Constraint |
| Build on established open source; borrow ideas from young projects, don't depend on them | Constraint |

## Decisions made

- **Lima is the VM layer on every host.** Mac: `vz`. Linux: QEMU/KVM. Windows: Lima's
  HCS driver once it's ready. No Multipass; Windows waits for Lima if needed.
- **GPU: try Lima on Linux first.** Incus becomes a second `box` driver for Linux
  only if Lima can't pass the GPU through.
- **Strong isolation wins over GPU.** No WSL2 anywhere.
- **Ubuntu (current LTS) is the primary guest** and the only one we ship at first.
  Other distros, including non-systemd ones like Alpine, must be addable as profiles
  without changing `box`.
- **Tailscale** is acceptable for remote access.
- **`box` is written in Go.**
- devvm (MIT) is design reference only; exeuntu (Apache-2.0) is the package-list
  reference. Neither is a dependency. Docker Sandboxes is out (closed source, needs
  a Docker account).
- **Images are local to their host.** No relay, sync service or peer-to-peer
  copying between hosts; an image moves by exporting it to a file and copying
  that by hand (owner, 2026-11-22).
- Repo: `homebox` (working name), public, under my GitHub account.
- **Not decided yet:** the repo's license.
- **The build started on 2026-11-21** on the owner's word; phases 0 and 1 (the
  Mac host) are built.

## Architecture

```
MacBook ──ssh (LAN; later Tailscale)──▶ VM "main"   ◀─┐
                                        VM "proj2"  ◀─┤ host: Mac Mini │ Windows PC │ Linux box
                                                      └─ box ─▶ driver (Lima; maybe Incus later)
                                                         └─ host rules: port publishing + egress
```

### `box`, the command

Runs on the host. Draft commands:

| Command | Does |
|---|---|
| `box host init` | One-time host setup: checks the VM layer, installs egress rules, turns LAN publishing on or off |
| `box new NAME [--profile ubuntu] [--cpus --memory --disk]` | Creates and provisions a VM, assigns its SSH port and HTTP port range |
| `box ls` / `start` / `stop` / `rm` | Lifecycle |
| `box ssh NAME` | Shortcut into a VM |
| `box ports NAME` | Show or change published ports |
| `box ssh-config` | Prints the snippet for a laptop's `~/.ssh/config` (`ssh main`) |
| `box doctor` | Checks isolation: VM can reach the internet, not the host or LAN |
| `box image save NAME IMAGE` / `image ls` / `image rm` | Saves a VM as an image; lists or deletes images (phase 2) |
| `box new NAME --image IMAGE` | Creates a VM from an image instead of provisioning from scratch (phase 2) |
| `box image rebuild [--profile P]` | Rebuilds a profile's base image from its scripts (phase 2) |
| `box image export IMAGE FILE` / `image import FILE` | Writes an image to one file, or reads one back, for copying to another host by hand (phase 2) |
| `box sleep` / `wake` NAME | Pauses a VM with its memory saved, and resumes it (later) |
| `box autostart on` / `off` | Installs or removes a boot-time job on the host that starts again every VM that was running when the host went down (later) |

**Autostart.** `box start` marks a VM as one that should be running and `box stop`
clears the mark; a host shutdown or crash leaves it set. When the host boots,
before anyone logs in, the autostart job runs box as the owner, which starts
every marked VM the normal way, isolation first. The job is the host OS's own
boot-time service: launchd on macOS, systemd on Linux, a startup task on Windows.
Hosts using it don't use FileVault, which would hold the boot until someone
types the password. (A
VM put to sleep would be woken instead, once sleep exists.) Lima's own
`limactl autostart` isn't used: it would start the VM without box's isolation.

Later option: exe.dev-style `ssh mini new proj3` from the laptop, through a
dedicated host account whose SSH login can only run `box` (`ForceCommand`), never a
shell. Without it, creating a VM means being at the host.

**Drivers.** `box` talks to the VM layer through a small interface: create, start,
stop, delete, list, SSH endpoint, port forwards; later, save and restore images,
and sleep and wake. The Lima driver writes Lima configs
and calls `limactl`; nothing else in `box` knows about Lima.

**Language: Go** (confirmed by the owner). One static binary per OS (macOS, Windows, Linux),
nothing to install on the host but Lima and `box`. It's what Lima, Tailscale and
exe.dev's own tooling are written in. OS-specific work calls native tools: `pfctl`
and `sandbox-exec` on macOS, `nft` on Linux, `netsh`/PowerShell on Windows.
Alternatives considered: Bash (not native on Windows, so a second PowerShell
copy of everything), Make (a build tool, not a CLI), Node or Python (a runtime
on every host).

### Guest profiles

A profile is a folder: which image to boot, plus provisioning scripts.

```
guest/
  common/        distro-independent tool layer (POSIX sh): mise languages,
                 Claude Code and Codex installers, Playwright browsers
  ubuntu/        image + base packages (apt) + services (systemd)
  debian/ ...    later, same shape
```

- **Contract with `box`:** the profile yields a VM with sshd on port 22, a user with
  admin rights, and an exit code from provisioning. Package manager and init system
  (systemd, OpenRC, ...) are the profile's own business.
- Scripts are POSIX `sh` so they also run on Alpine's shell.
- Ubuntu profile: exeuntu's package list plus Node, Playwright and browsers, Xvfb,
  Docker, Claude Code, Codex, Tailscale (off by default). Optional desktop layer later.

### Images

- An image is a saved VM disk plus the record `box` needs to start it (profile,
  size). Your images live on the host under box's own state, never on a shared or
  cloud store.
- **Base images.** The first `box new` with a profile provisions a throwaway VM
  with that profile's scripts, before anything is logged in, and saves it as
  the profile's base image. Later `box new` clones the base image, gives the VM
  its fresh identity, and runs a short update step (Claude Code, Codex, OS
  packages) instead of the full scripts. The scripts stay the only definition of
  what's in a VM; the base image is a cache of their output, refreshed with
  `box image rebuild`, and rebuilt automatically when the profile changes.
- **Later, maybe: a published base image.** CI builds the same base image and
  publishes it on GitHub Releases, signed, so the first `box new` downloads it
  instead of provisioning. Open questions before doing this: GitHub's 2 GB file
  limit, one image per distro and CPU type, and whether Claude Code may be
  redistributed inside an image.
- A VM made from an image gets a fresh machine ID, hostname and SSH host keys on
  first boot, and box pins the new keys, so two VMs from one image never share
  an identity.
- An image carries whatever was on the disk, including agent logins and tokens.
  `box image save` says so; every VM made from it starts signed in.
- Isolation is unchanged: a VM from an image gets the same egress rules, ports
  and no-mounts config as a freshly provisioned one.

### Isolation and networking

- VM config: no mounts, no SSH agent forwarding, no host loopback access.
- **Egress rules, enforced on the host** (root in the VM can't undo them): the VM
  reaches the public internet, not the host or private address ranges.
  Mac: a Seatbelt profile around Lima's network process (devvm's approach).
  Linux: nftables. Windows: Windows Firewall (least certain).
- **Inbound:** each VM's SSH on a host port (22NN) and an HTTP range, published on
  the LAN only where the host opts in. SSH lands in the VM; the host takes no SSH
  logins (except the optional `box`-only account above).
- **Credentials:** at first, agent logins live in the VM and the GitHub token is
  fine-grained and scoped to chosen repos. Later, a host-side credential proxy
  (devvm's design) keeps them out of the VM.

## Phases

| # | Goal | Exit criterion |
|---|---|---|
| 0 | **Spikes on the Mac** (about a day): Lima config, SSH published on the LAN, Seatbelt egress rule | SSH into a VM from the MacBook; it reaches the internet but not the Mini, its loopback or my router |
| 1 | Mac, done properly: `box` with the Lima driver, Ubuntu profile, host init, ssh-config, doctor, CI | From the MacBook: `ssh main`, agents run with full permissions on my accounts, Playwright screenshots a dev server I also open on the laptop; the same `ssh main` and dev server from the Mac itself; `box doctor` passes |
| 2 | Multiple VMs, images and base images, Tailscale, desktop (Xfce over VNC or RDP) | Two VMs side by side, the second started from an image of the first; a new VM from the base image skips the full scripts; SSH from off my network; desktop from the laptop |
| 3 | Credential proxy | Agent tokens unreadable inside the VM |
| 4 | Windows host, when Lima's HCS driver is ready | Phase 1's exit criterion on the Windows PC |
| 5 | Linux GPU box: Lima with the 3080 Ti passed through; Incus driver if Lima can't | `nvidia-smi` and a CUDA job in an isolated VM |
| 6 | Later | Sleep and resume, autostart when the host boots, a published base image, `box`-only SSH account, more guest profiles, Android, macOS guests (iOS and Mac apps), Windows guests |

## Open risks

- **Lima on Windows** is experimental (one VM at a time, static port forwards only);
  phase 4 waits for it.
- **Egress rules on Windows** with the Windows Firewall are unverified.
- **GPU passthrough with Lima** has no first-class option (extra QEMU arguments or
  a driver plugin, unverified); Incus is the fallback.
- **Seatbelt on macOS** is deprecated-but-working Apple tech; devvm relies on it too.
- Nested virtualization needs an M3+ Mac, so Android waits for hardware or the Linux box.
- **Images on Lima:** `limactl clone` copies a stopped VM; whether box clones
  through it or copies the disk itself, and how fast that is on each driver, is
  for a spike. At first box saves images from a stopped VM.
- **Sleep and resume on Lima** has no `limactl` command; Apple's framework can
  save a running VM's state (macOS 14+), and Lima's `vz` driver links that API,
  but whether Lima exposes it is unverified. QEMU has its own save and restore.
- **Autostart at boot on macOS:** whether Apple's Virtualization framework runs a
  VM from a boot-time launchd job, with nobody logged in, is unverified. If it
  needs a user session, the fallback is automatic login, which works without
  FileVault.
- **macOS guests:** Lima 2.3 ships macOS guest templates (`vz` only). Apple's
  licence allows two running per Mac. Setting a macOS guest up without clicking
  through Apple's setup screens is unexplored here (localbox shows it can be done).
