# Off-the-shelf alternatives to homebox

What existing software could do instead of building homebox, how well each
option covers the requirements in [plan.md](plan.md), and where it falls short.
Researched on 2026-11-22 from each project's own docs where possible; these
products change quickly, so check a project's current docs before relying on a
row here. This page is research, not a decision: [plan.md](plan.md) holds the
decisions.

## Summary

**No off-the-shelf product covers all of homebox's requirements.** The closest
for a single machine is **Proxmox VE**, a server operating system for VMs. On a
dedicated x86 PC (for example a spare drive in the gaming PC) it covers nearly
every requirement, including GPU passthrough. But Proxmox *is* the operating
system: it can never run on a Mac, or alongside Windows.

For a Mac or Windows machine as the host, the only options are desktop VM apps
set up by hand for each VM, or agent sandboxes (Docker Sandboxes, localbox)
that agents drive through their own tools rather than long-lived boxes you SSH
into from another machine. That gap is why homebox
exists.

## Comparison

| Approach | Mac host | Windows host | SSH into the VM from your network | Isolation from host | Block VM from host and home network | GPU | Open source / free |
|---|---|---|---|---|---|---|---|
| **Proxmox VE** | ❌ | ❌ (replaces the OS) | ✅ each VM gets its own address on your network | ✅ full VMs | ✅ per-VM firewall rules, applied on the host | ✅ best of any option | ✅ open source, free without support |
| **Desktop VM apps, set up by hand** (Hyper-V, UTM, VMware) | ✅ UTM or VMware Fusion | ✅ Hyper-V or VMware Workstation | ✅ with bridged networking | ✅ | ⚠️ manual, or not possible | ❌ | ✅ free (UTM is open source) |
| **OrbStack isolated machines** | ✅ only | ❌ | ⚠️ unverified | ⚠️ machines share one Linux kernel | ⚠️ can block the host; home network unclear | ❌ | ❌ closed source, free for personal use |
| **Docker Sandboxes** | ✅ | ✅ | ❌ SSH is for editors on the same machine | ✅ microVM per sandbox | ✅ network rule presets | ⚠️ docs mention it; unverified | ❌ closed source, needs a Docker sign-in |
| **localbox** | ✅ only (Apple Silicon) | ❌ | ❌ agents use its CLI or MCP; no SSH for you, no port forwarding | ✅ full VMs | ❌ doesn't block the Mac's own services | ❌ | ⚠️ free, but no license, and remote use runs through its hosted relay |
| **Coder** (self-hosted) | ⚠️ workspaces are Docker containers | ⚠️ same | ✅ through its own CLI | Depends on the template | Depends on the template | Depends on the template | ✅ open source |
| **boxd** (self-hosted) | ❌ | ❌ | ✅ | ✅ microVMs | Not documented | ❌ | ⚠️ free up to 16 vCPUs, telemetry on, vendor-signed releases |
| **svkexe** (exe.dev clone) | ❌ | ❌ | ✅ SSH gateway | ⚠️ containers, not VMs | Not documented | ❌ | ✅ open source, but 13 stars and one maintainer |
| **Unraid** | ❌ | ❌ (replaces the OS) | ✅ | ✅ | ⚠️ | ✅ | ❌ $49–$249 one-time |
| **homebox** (for comparison) | ✅ built (phase 1) | Planned, waits for Lima's Windows driver | ✅ | ✅ full VMs, no mounts | ✅ enforced on the host | Planned (Linux host) | ✅ |

✅ covers it · ⚠️ partly, with caveats, or unverified · ❌ doesn't

## The options in detail

### Proxmox VE

**What it is.** A free, open-source (AGPL) server operating system based on
Debian, made by Proxmox Server Solutions in Austria. You install it on a whole
drive from an ISO, and it runs VMs (KVM) and Linux containers (LXC), managed
from a web interface. It has been developed since 2008 and has a large homelab
community. The current release is 9.2 (May 2026), on Debian 13.

**What it does well:**

- **GPU passthrough** is documented and widely used, including with consumer
  NVIDIA cards. A passed-through GPU belongs to one VM at a time: "you cannot
  use that device anymore on the host or in any other VM." It needs IOMMU
  (Intel VT-d or AMD-Vi) turned on in the BIOS, and works best with the q35
  machine type and UEFI firmware. CUDA (compute) needs no display. For graphics,
  Proxmox's web console can't show a passed-through GPU's screen, so you'd use
  VNC or RDP inside the VM, or plug in a monitor.
- **Per-VM firewall.** Each VM can have incoming and outgoing rules, with a
  default outgoing policy, enforced by the firewall service on the host. The
  rule syntax can express "internet yes, host and home network no" (drop
  outgoing traffic to private address ranges). The docs don't show that exact
  recipe, and don't state outright that the guest can't change the rules; since
  the rules live on the host, it can't (inference).
- **Fast VM creation.** Cloud-init support (user, SSH keys, network config)
  plus templates and linked clones give a new VM in seconds.
- **Automation.** A REST API (`https://host:8006/api2/json/`) with API tokens,
  plus the `pvesh` and `qm` command-line tools. Terraform providers exist.
- **Networking.** VMs can sit directly on your home network through a bridge,
  each with its own address, so `ssh vm` works without port forwarding.
  Tailscale installs inside each VM as usual.
- **Any guest:** any Linux distro, and Windows guests, including Windows with
  the GPU passed through.
- **Consoles:** noVNC in the browser, SPICE through a viewer app, or a serial console.

**What it doesn't do:**

- **x86 PCs only, in practice.** Arm64 is officially supported only on NVIDIA
  Grace Hopper and Vera server platforms, best-effort on other modern Arm
  servers, and not at all on Raspberry Pi. It can't run on a Mac.
- **It replaces the operating system.** Installation erases the chosen drives.
  On a dual-boot gaming PC, the machine isn't a Windows PC while the Proxmox
  drive is booted.
- **Nothing for coding agents.** No agent images, no protection for agent
  logins, no SSH shortcuts. You'd still need a setup script for the VM (like
  homebox's Ubuntu profile) and a laptop SSH config.
- **The free tier is unsupported.** Without a subscription you use the
  "no-subscription" package repository, which Proxmox describes as for testing
  and non-production use, and the web UI shows a "no valid subscription"
  notice. Subscriptions start at €120 per CPU socket per year (Community, no
  support tickets) and go up to €1,100 (Premium). Every tier has the same
  features.
- **Setup effort.** About an afternoon: install, BIOS settings for passthrough,
  binding the GPU to VFIO, building a cloud-init template.

**Fit:** the strongest off-the-shelf choice for a dedicated Linux/GPU machine.
No use for the Mac or Windows hosts.

### Desktop VM apps, set up by hand

**What it is.** The ordinary VM apps for each desktop OS, with each VM
configured manually:

- **Windows:** Hyper-V (built into Windows Pro; "Quick Create" offers Ubuntu
  images) or VMware Workstation Pro (free since 2024).
- **Mac:** UTM (open source, built on QEMU and Apple's Virtualization
  framework), VMware Fusion (free since 2024) or Parallels (paid).

**What it does well:**

- Covers every must-have today with no code: a full Linux VM with strong
  isolation, SSH from other machines (put the VM on your network with
  Hyper-V's external switch or bridged networking), and agents installed by
  hand.
- Free, well supported, and familiar.

**What it doesn't do:**

- **Every step is by hand, for every VM:** install the OS, install the tools,
  set up SSH keys, configure networking. Nothing is repeatable.
- **No network rules on the host.** Blocking the VM from the host and your home
  network has to be built from the host firewall yourself; with a bridged
  network it's hard to do at all, because the VM is a full device on your
  network.
- **A different tool on each OS**, with different settings and quirks.
- **No GPU for Linux guests**, in practice, on either OS.

**Fit:** a fine way to start by hand; it's what homebox automates.

### OrbStack

**What it is.** A polished, closed-source Mac app for Docker containers and
Linux "machines." Free for personal use; paid for business.

**What it does well:**

- Very fast and light, with good Mac integration.
- **Isolated machines** (`orb create --isolated`) don't mount the Mac's files,
  can't reach the Mac over the network or run Mac commands, and don't forward
  your SSH agent. Selective folder mounts for isolated machines arrived in
  April 2026. A separate setting isolates a machine's network.

**What it doesn't do:**

- **Weaker boundary.** All machines are containers sharing one Linux kernel
  inside one VM. OrbStack's docs say isolation relies on the Linux kernel's
  security model, with extra hardening. That's weaker than a separate VM per
  box.
- **Mac only** and closed source.
- Reaching a machine over SSH from another computer on your network, and
  blocking a machine from your home network, are unverified.
- No GPU.

**Fit:** the best Mac experience, with a weaker boundary than homebox's design
requires.

### Docker Sandboxes

**What it is.** Docker's product for running coding agents in isolation, with
the `sbx` command-line tool. Each sandbox is a microVM with its own kernel and a
private Docker daemon. It runs on macOS (Apple silicon), Windows 11 and Linux,
without Docker Desktop. The local version is free; a cloud version on Docker's
servers is paid.

**What it does well:**

- **Strong agent-safety features out of the box:** a microVM per sandbox,
  network policy presets (allow all, balanced, deny all) with rules by host,
  port and HTTP path, and agent logins kept on the host with placeholders in the
  sandbox.
- Supports Claude Code, Codex, Copilot, Cursor, Gemini, OpenCode and others.
- Editors can connect to a sandbox over SSH.

**What it doesn't do:**

- **Closed source**, and needs a Docker sign-in.
- **Built around agent sessions, not a long-lived box.** Files outside the
  mounted project folder live on the sandbox's own disk and are lost when the
  sandbox is removed.
- **Not reachable from another machine.** Its SSH is for editors on the same
  computer.
- GPU passthrough appears in its docs' table of contents; unverified here.

**Fit:** the best ready-made agent sandbox on all three operating systems, but
not an exe.dev-style box, and not open source.

### localbox

**What it is.** A Mac app that turns each of your Macs into sandboxes for coding
agents ([github.com/dzhng/localbox](https://github.com/dzhng/localbox)). Each
sandbox is a full Linux or macOS VM on Apple's Virtualization framework, with a
shell, files, a desktop and Chrome. Your agent creates and drives sandboxes
with the `localbox` CLI (`sandbox create`, `fs read`, `browser click`,
`desktop screenshot`, ...) or the same operations over MCP. The host side is
Swift; the CLI, relay and guest agent are TypeScript. It started in October
2026, has one maintainer, and had 2 stars on 2026-11-22, with daily commits.
Its Linux image uses exe.dev's own VM package set, ported to Arm64, plus an
XFCE desktop and Chrome.

**What it does well:**

- **Images.** Save a running sandbox as an image (it pauses for about a second),
  make one the default, and start new sandboxes from it. A sandbox made from an
  image regenerates its machine ID and SSH host keys on first boot. Images copy
  directly between your Macs, peer to peer.
- **Sleep and resume.** A sandbox idle for 30 minutes goes to sleep, and the next
  request wakes it with every process where it was, dev servers included.
  Sandboxes can instead delete themselves after an idle timeout.
- **macOS guests.** It builds a macOS VM from Apple's restore image with nothing
  to click through (SIP is turned off inside the guest so its agent can grant
  itself screen and input permissions). Xcode is installed once with your Apple
  ID and saved as an image. Needs macOS 27 on the host; Apple's licence allows
  two macOS VMs running per Mac.
- **Several Macs as one pool**, with placement on whichever has room.
- **Built-in desktop and browser operations** for agents: screenshot, click,
  type and scroll on a 1440×900 display.

**What it doesn't do:**

- **No SSH for you.** It's built for agents, and its pitch is "not SSH". You
  reach a sandbox through its CLI or MCP, and there's no port forwarding, so a
  dev server in a sandbox isn't reachable from your laptop's browser.
- **No blocking of the host or home network.** Sandboxes share Apple's NAT
  network, and its security doc says it doesn't firewall them from each other or
  from services the Mac listens on. Its own Xcode guide has the sandbox download
  a file from a server on the Mac.
- **A hosted relay.** Remote agents and other Macs reach your sandboxes through
  localbox's own service (Vercel and Convex), with an emailed sign-in code.
  Requests and results sit there as plain JSON that the operator can read. Use
  on the same Mac doesn't go through the relay, but creating a sandbox needs the
  sign-in.
- **Mac only**, one Ubuntu image, no GPU.
- **No license file**, so by default nobody else has the right to reuse the
  code.

**Fit:** a different product: sandboxes for agents, not a box you SSH into. Its
images, sleep and resume, and macOS guests are ideas homebox adopts (see
[plan.md](plan.md)), built on Lima rather than its code.

### Coder

**What it is.** An open-source (AGPL) platform for remote development
environments ("workspaces"), from Coder Technologies. You run the Coder server
yourself; each workspace is defined by a Terraform template: a Docker
container, a VM through a Proxmox or libvirt provider, a cloud VM, or a
Kubernetes pod.

**What it does well:**

- **Can run entirely on the host,** so there's no outside server. Its docs
  suggest a single-machine install with Docker workspaces for trying it out.
- Mature, with a web dashboard, `coder ssh`, editor integrations, and built-in
  agent features that keep LLM credentials in the server rather than in
  workspaces.
- Templates can target Proxmox, so Coder can sit on top of a Proxmox machine.

**What it doesn't do:**

- **Isolation depends on the template.** The easy path, Docker containers, is a
  weaker boundary than a VM.
- **Heavy for one person:** a server process, a database, Terraform templates.
- **Changes the SSH workflow** to go through `coder ssh` or its SSH config,
  rather than plain SSH straight into the VM.
- Nothing ready-made for blocking a workspace from the host and home network.

**Fit:** a team platform; more machinery than one person needs.

### Products modelled on exe.dev

- **exe.dev itself** has no self-hosted version. Running on your own cloud is
  offered only on enterprise plans.
- **boxd** runs persistent KVM microVMs over SSH, with Claude Code, Codex and
  OpenCode preinstalled. It can be self-hosted on AWS, Google Cloud or any Linux
  machine with hardware virtualization, using an open-source installer. It's
  free up to 16 worker vCPUs, with hourly usage telemetry on by default, and
  upgrades come as vendor-signed release bundles. It doesn't document a Mac or
  Windows host, a desktop or GPU.
- **svkexe** is a self-hosted exe.dev clone (Apache-2.0): a Go gateway, web
  dashboard and SSH gateway over Incus system containers. Its own README says
  these are "LXC system containers sharing the host kernel, not
  hardware-virtualized guests." It needs an Ubuntu or Debian host and your own
  domain, and is a small single-maintainer project (13 stars, 121 commits).
- **boat** offers full VMs with a desktop and an agent API, but only as a paid
  hosted service in Europe. There's no self-hosted version.

**Fit:** none runs on a Mac or Windows host; boxd is the most capable, as a
vendor product on Linux servers.

### Projects to avoid

- **DevPod** (Loft Labs): client-only dev environments using devcontainers, with
  no server. The newest release found is from March 2025, and maintenance is in
  doubt.
- **Daytona:** its open-source repository was archived in June 2026, when
  development moved to a private codebase. A community fork, Nightona,
  continues the last open release (AGPL-3.0).

### Other server operating systems

All are in the same category as Proxmox: they replace the host OS.

- **Unraid:** a NAS operating system with VMs and GPU passthrough. Paid,
  one-time: $49 (Starter) or $249 (Lifetime). Built around storage first.
- **TrueNAS Community Edition:** a NAS operating system whose VM system changed
  from libvirt to Incus in 25.04, then back to libvirt in 25.04.2, so its VM
  support has been unstable.
- **XCP-ng:** the open-source Xen hypervisor with the Xen Orchestra web UI.
  Supports passing a whole NVIDIA GPU to a VM; NVIDIA's vGPU sharing isn't
  supported.
- **Harvester (SUSE):** hyperconverged infrastructure built on Kubernetes. Needs
  at least 8 cores and 32 GB of RAM; aimed at clusters.
- **IncusOS:** an immutable OS that only runs Incus, released November 2025.
  It has no shell, by design, and is managed through the Incus API.

Proxmox has the largest community and the best GPU passthrough record of these.

## What this means for homebox

- **Nothing here replaces homebox on the Mac and Windows hosts.**
- **Proxmox is a serious candidate for the Linux GPU box** (phase 5 in
  [plan.md](plan.md)), possibly better than Lima on Linux. GPU passthrough,
  per-VM firewall rules and a web console are built in, and `box` could talk to
  Proxmox's REST API as a second VM driver alongside Lima. Choosing it would
  change the design, so it needs the owner's decision; it isn't needed before
  phase 5.
- **Borrowable ideas:** Docker Sandboxes' and devvm's host-side credential
  proxy (homebox's planned credential proxy), Coder's SSH config integration,
  and Proxmox's cloud-init template and linked-clone workflow.
- **Adopted from localbox** (owner, 2026-11-22): images, sleep and resume, and
  macOS guests are now in [plan.md](plan.md). Images stay on the host that made
  them; homebox won't run a relay to copy them between hosts.

## Sources

- **Proxmox:**
  [pricing](https://www.proxmox.com/en/products/proxmox-virtual-environment/pricing),
  [package repositories](https://pve.proxmox.com/wiki/Package_repositories),
  [PCI(e) passthrough](https://pve.proxmox.com/wiki/PCI(e)_Passthrough),
  [firewall](https://pve.proxmox.com/wiki/Firewall),
  [system requirements](https://pve.proxmox.com/wiki/System_Requirements),
  [installation](https://pve.proxmox.com/wiki/Installation),
  [cloud-init](https://pve.proxmox.com/wiki/Cloud-Init_Support),
  [API](https://pve.proxmox.com/wiki/Proxmox_VE_API),
  [VM chapter](https://pve.proxmox.com/pve-docs/chapter-qm.html),
  [9.2 release](https://proxmox.com/en/about/company-details/press-releases/proxmox-virtual-environment-9-2),
  [Wikipedia](https://en.wikipedia.org/wiki/Proxmox_Virtual_Environment)
- **Proxmox GPU guides:**
  [Proxmox forum guide](https://forum.proxmox.com/posts/858397/),
  [RTX passthrough gist](https://gist.github.com/KasperSkytte/6a2d4e8c91b7117314bceec84c30016b),
  [Virtualization Howto](https://www.virtualizationhowto.com/2023/10/proxmox-gpu-passthrough-step-by-step-guide)
- **Desktop VMs:**
  [OrbStack isolated machines](https://docs.orbstack.dev/machines/isolated),
  [OrbStack architecture](https://docs.orbstack.dev/architecture),
  [OrbStack release notes](https://docs.orbstack.dev/release-notes),
  [Mac VM comparison](https://dev.to/michael_sun_18a5c4c96768d/utm-vs-orbstack-vs-lima-macos-vm-real-cold-start-disk-io-and-memory-numbers-on-m3-macbook-pro-4j6d),
  [Hyper-V Linux VM tools](https://github.com/microsoft/linux-vm-tools/wiki/Onboarding:-Ubuntu),
  [Ubuntu 24.04 on Hyper-V](https://dev.to/klo2k/create-ubuntu-24042-vm-in-hyper-v-with-enhanced-session-rdp-support-windows-11-xrdp-1omk)
- **Docker Sandboxes:**
  [docs](https://docs.docker.com/ai/sandboxes/),
  [cloud sandboxes announcement](https://www.docker.com/blog/introducing-cloud-sandboxes-start-on-your-laptop-finish-in-the-cloud/),
  [University of Cambridge guide](https://guidebook.devops.uis.cam.ac.uk/explanations/development/docker-sandboxes-for-agentic-ai/)
- **localbox:**
  [README](https://github.com/dzhng/localbox/blob/main/README.md),
  [security model](https://github.com/dzhng/localbox/blob/main/SECURITY.md),
  [development guide](https://github.com/dzhng/localbox/blob/main/DEVELOPMENT.md),
  [images guide](https://github.com/dzhng/localbox/blob/main/skills/localbox/references/images.md)
- **Dev environment platforms:**
  [Coder docs](https://coder.com/docs/@main),
  [Coder homelab post](https://coder.com/blog/run-coder-in-a-self-hosted-homelab),
  [DevPod releases](https://www.github.com/loft-sh/devpod/releases),
  [Daytona status](https://www.agent37.com/blog/daytona-self-hosted),
  [Nightona fork](https://cdn.jsdelivr.net/gh/nightona-co/nightona@main/README.md)
- **exe.dev-like products:**
  [svkexe](https://github.com/skrashevich/svkexe),
  [boxd self-hosting](https://boxd.sh/self-hosting/),
  [boxd vs exe.dev](https://boxd.sh/blog/boxd-vs-exe-dev/),
  [boat vs exe.dev](https://boat.dev/compare/boat-vs-exe-dev),
  [exe.dev alternatives](https://agentsandboxes.org/alternatives/exe-dev/)
- **Other server operating systems:**
  [Unraid pricing](https://unraid.net/pricing),
  [TrueNAS virtualization plans](https://forums.truenas.com/t/truenas-virtualization-plans-for-25-04-2/46236),
  [XCP-ng compute docs](https://docs.xcp-ng.org/compute),
  [Harvester requirements](https://docs.harvesterhci.io/v1.9/install/requirements),
  [IncusOS announcement](https://stgraber.org/2025/11/07/introducing-incusos/)
