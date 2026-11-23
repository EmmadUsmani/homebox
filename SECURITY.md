# Security model

homebox exists so coding agents can run with full permissions on a machine that
also holds personal accounts, a browser, email and banking. This page says what
homebox protects, how, and what it doesn't try to protect. It describes what is
built today: the Mac host (phases 0 and 1). Windows and Linux hosts don't exist
yet, and their controls will differ.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: the repository's **Security** tab,
then **Report a vulnerability**. Please don't open a public issue for anything
that weakens the isolation below.

## What's protected

The **host**: its files, accounts, keys, browser sessions, running apps and
local services. And the **home network**: the router and every other device on
it.

Everything inside the VM is untrusted. Agents run there with full permissions
and root, and anything they install, download or are tricked into running is
assumed hostile. The aim is that root in the VM can't reach the host or the home
network, and can't undo the rules that stop it.

## The boundaries

### The VM and the host

- **A full VM.** Each VM is its own Linux kernel and disk under Apple's
  Virtualization framework, through Lima's `vz` driver. It's not a container.
- **Nothing shared.** No mounts of host folders, no SSH agent forwarding and no
  X11 forwarding: the guest's sshd refuses both, whatever the client asks for
  ([`guest/ubuntu/base.sh`](guest/ubuntu/base.sh)).
- **No host loopback, no host DNS.** Lima's host-loopback access and host
  resolver are turned off, so the VM can't reach services listening on the
  Mac's `localhost`.
- **The host processes are confined.** Lima's process for the VM and box's
  per-VM network daemon each run under a Seatbelt profile
  ([`internal/confine`](internal/confine)). These profiles limit which network
  addresses they can connect to and listen on, which files they can read or
  write, and which executables they can run. Read access covers required
  macOS libraries and configuration, VM state, and the process executables.
  Ordinary files on mounted volumes, in `/Users/Shared`, and in host temporary
  directories are outside that allowlist. Only Lima can look up named Mach
  services, for Virtualization.framework, its cache directory lookup and local
  account resolution.
- **The host takes no SSH logins for box.** You SSH into the VM, never into
  the Mac.

### The VM and the network

The VM reaches the internet through box's own gateway on the host, not through
Lima's networking ([architecture](docs/architecture.md#networking-and-isolation-macos)).

- Every new connection's destination is checked on the host, against
  [`internal/egress/policy.go`](internal/egress/policy.go). Private ranges,
  loopback, link-local, carrier-grade NAT (which includes Tailscale's
  addresses), multicast and reserved ranges are refused. So are the networks of
  the host's own interfaces, whatever their range.
- The check runs on the host, so root in the VM can't change it by editing
  routes, DNS or firewall rules in the guest.
- IPv4 TCP, UDP and ICMP echo are allowed to public addresses; IPv6 is dropped.
- Flows are capped per VM: a VM that opens connections too fast is refused, not
  given the host's whole network stack.

### Your laptop and the VM

- SSH lands in the VM, through a relay box runs only while the VM's host process
  is alive. Before it dials the driver's loopback listener and again before
  it forwards traffic, box checks the listener and host process identities.
  Unverified ownership closes the connection and produces a rate-limited daemon
  warning. The VM's sshd drops a client that stops answering (within about
  two minutes), so a laptop that sleeps, leaves or crashes doesn't leave
  sessions open in the VM. Lima's port forwarder doesn't pass a closed
  connection on to the guest, so without this they would stay open forever.
- Laptops pin each VM's SSH host keys, so an impostor on the port fails the
  host-key check. `box repin` changes pins only after you review them.
- Ports are published only on the Mac's loopback unless you turn on LAN
  publishing (`box host init --lan`), per host.

### Checking it

`box doctor NAME` tests the boundary from inside a running VM. It checks that
the VM reaches the internet but not the host's loopback services, its LAN
interfaces or the router. It also checks both installed profiles for denied
home reads and writes, reads from `/Volumes`, `/Users/Shared` and host temporary
directories, and lookup of the macOS notification service, and the
`ssh-relay-target-owner` check verifies the driver SSH listener on the host.
Run it after any change to the host or the VM's network. Restart running VMs
after upgrading box so their host processes pick up the current profiles.

`box status` and `box top` pull data over the existing SSH connection with
bounded output and timeouts. The guest's answers are untrusted and used only
for display. Box strips terminal controls and opens no listener or guest-to-host
reporting channel for these commands.

### Desktop app

The packaged desktop app uses WKWebView's in-process Wails asset and binding
handlers with embedded frontend files. Local status and logs use the same
bounded, pinned SSH reads as the CLI and render guest content as text. Starting
a VM launches the bundled `box` network daemon under the same per-VM Seatbelt
profile as a CLI start. The app serves no network API. Running doctor explicitly
opens its short-lived diagnostic canaries, as in the CLI. The Vite development
server listens only on host loopback and serves frontend files.

Remote status runs the bounded, read-only guest probe over the laptop's own
SSH login. Each check verifies the pinned key in `homebox_known_hosts` and
disables agent forwarding, X11 forwarding, port forwarding and SSH
connection sharing.

The terminal uses box's stored pins for loopback SSH to local VMs and the
installed homebox pins for remote aliases. Host key changes block the
connection. Remote authentication tries the local SSH agent and unencrypted
default identity files without copying private keys or forwarding the agent
or X11. Terminal output is parsed by xterm.js with bounded buffering and
scrollback. Guest escape sequences cannot set the clipboard or open links.

The Files tab uses SFTP over the same pinned SSH pool. Guest names and file
contents are untrusted text. The browser reads at most 5,001 directory
entries per request and limits paths to 4,096 bytes. The editor reads and
accepts at most 2 MiB of UTF-8 text and rejects NUL bytes. Monaco and its
workers are bundled locally. Native dialogs authorize the source of each
upload and the destination of each download.

Writes check for symbolic links in the target path. Editor saves stage a
same-directory file, preserve the mode and recheck the original's size and
SFTP modification time before renaming over it. If the server refuses the
rename, the save fails. SFTP exposes second-resolution timestamps and has
no conditional rename, so edits with identical size and timestamp or
changes between the final check and rename can escape conflict detection.

Phase 2's remote desktop is a proposal. The [display-server spike](docs/spikes/remote-desktop.md#authentication-and-data-channels)
records loopback listener checks, the KasmVNC UDP bind caveat and proposed
browser session and clipboard controls.

### Logs

Box treats guest journal entries, provisioning output and host log fields
that can carry guest data as untrusted text. It bounds line sizes, strips
terminal escape sequences and control characters, and displays the contents
only. Per-run provisioning logs are readable only by the host owner.

Box opens guest journal sessions over pinned SSH, including when following
new entries. It accepts bounded journal cursors with a restricted character
set solely to resume a disconnected stream. Ubuntu provisioning adds the
VM user to the `systemd-journal` group, granting read access to the system
journal without running the log reader as root.

## Secrets in the VM

Anything in the VM is readable by the agents in it, including the agents' own
logins.

- Log Claude Code and Codex in yourself, inside the VM. Those logins live there.
- Give the VM a fine-grained GitHub token scoped to the repositories it needs,
  never a personal token with full access.
- A host-side credential proxy that keeps tokens out of the VM is planned
  (phase 3 in the [design](docs/plan.md)).
- Images, when they exist (phase 2), will carry whatever was on the disk,
  logins included. Base images are built before anything is logged in.

## Tailscale inside the VM

Tailscale is installed in the VM but off. If you turn it on, the VM joins your
tailnet over the internet, which the egress rules allow. It can then reach
whatever your tailnet's access rules let it reach, including your other devices.
Restrict the VM with tailnet access rules (for example, a tag that can accept
connections but not open them) before turning it on.

## Not covered

- **A bug in the hypervisor** that lets a guest escape Apple's Virtualization
  framework. homebox relies on Apple here, and on the Seatbelt profiles to
  limit the damage.
- **A compromised host, or someone with physical access.** Hosts that use
  autostart run without FileVault (the owner's choice), so the disk, VM disks
  included, isn't encrypted at rest.
- **Isolation between VMs on one host** hasn't been checked yet: several VMs
  per host is phase 2.
- **Accepted limits** of the gateway, such as routers that reflect traffic to
  their public address back into the LAN, are listed under "Known limitations"
  in the [architecture](docs/architecture.md#networking-and-isolation-macos)
  and tracked in the [to-do list](docs/todo.md).
- **What the agents do on the internet** with your accounts. The VM can reach
  any public address by design.
