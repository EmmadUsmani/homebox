# Security model

homebox exists so coding agents can run with full permissions on a machine that
also holds personal accounts, a browser, email and banking. This page says what
homebox protects, how, and what it doesn't try to protect. It describes the controls on macOS and Linux.

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

### Browser processes inside the Ubuntu guest

Ubuntu provisioning allows unprivileged user namespaces so Chromium and
Electron can sandbox their renderers when agents browse untrusted sites.
This opens additional guest kernel attack surface to unprivileged code.
The development account already has passwordless sudo and root-equivalent
Docker group membership. The hypervisor, host network rules and host process
confinement enforce the VM-to-Mac boundary independently. See the
[decision and alternatives](docs/decisions.md#browser-sandboxes-inside-the-ubuntu-guest).

### The VM and the macOS host

These controls apply to homebox's Ubuntu guests. Lima's experimental macOS
guest templates have a separate networking and bootstrap path.
See the [macOS guest spike](docs/spikes/macos-guests.md).

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

When restoring an image into a fresh state directory, `box` generates the
dedicated Lima login key before launching the confined host agent.
Image restores use the same host-agent file, process and network permissions.

### The VM and the Linux host

- **QEMU/KVM** runs the VM through Lima. The SSH NIC uses SLIRP with
  `restrict=on` and `ipv6=off`. QEMU receives the separate gateway
  connection as an inherited Unix socket. A seccomp filter denies QEMU
  outbound connects, IPv6 sockets and datagram sockets. Unknown network
  arguments abort launch. The QEMU and hostagent filters also reject
  Fast Open sends and batched sends.
- **Landlock** limits hostagent, QEMU and the gateway daemon to their
  file allowlists. QEMU can write its own disk and selected runtime files.
  The gateway daemon writes only in its own run directory. The launchers
  set `no_new_privs`, scope abstract Unix sockets and signals, and install
  seccomp filters before entering the host processes. A failed filter
  aborts launch. Landlock limits the hostagent’s TCP connects by port.
  Its rules cannot restrict the destination IP or the pathname Unix
  socket destinations used by the hostagent and gateway.
- **SSH** publishes through the gateway daemon. The daemon matches the
  Unix SSH peer's kernel pidfd to the QEMU launcher identity and watches
  the process for exit. Lima's own SSH forward stays on IPv4 loopback.
  The LAN switch controls the published SSH port and static TCP forwards.
- **Doctor** checks the live QEMU command and SSH listener owners, probes
  each confinement policy, and tries the host and router canaries through
  both NICs. See [Linux isolation limits](docs/architecture.md#networking-and-isolation-linux).

### The VM and the network

The VM reaches the internet through [box's own gateway](docs/architecture.md#networking-and-isolation-macos) on the host.

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

If the per-VM gateway daemon exits, egress stays blocked. Stop and start
the VM through box to establish a fresh confined gateway connection.

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

Local lifecycle actions use the same host service and isolation setup as
`box`. Provisioning output is cleaned for display and rendered as plain text.
The delete action requires the exact VM name and removes the Lima disks through
the shared remove operation.

The packaged desktop app uses WKWebView's in-process Wails asset and binding
handlers with embedded frontend files. Local status and logs use the same
bounded, pinned SSH reads as the CLI and render guest content as text. Starting
a VM launches the bundled `box` network daemon under the same per-VM Seatbelt
profile as a CLI start. The app serves no network API. Running doctor explicitly
opens its short-lived diagnostic canaries, as in the CLI. The Vite development
server listens only on host loopback and serves frontend files.

Settings changes published ports through the same stopped-VM validation and
host binding policy as `box ports`. Opening or copying a mapped URL uses the
host loopback address. Local SSH host-key replacement requires reviewing saved
and detected fingerprints and explicitly confirming an intentional change.
The shared re-pin operation checks both fingerprint lists again before saving.

Remote status runs the bounded, read-only guest probe over the laptop's own
SSH login. Each check verifies the pinned key in `homebox_known_hosts` and
disables agent forwarding, X11 forwarding, port forwarding and SSH
connection sharing.

The terminal uses box's stored pins for loopback SSH to local VMs and the
installed homebox pins for remote aliases. The SSH transport prefers pinned
key types during negotiation and still requires an exact pin match. Host key
changes block the connection. Remote authentication tries the local SSH agent
and unencrypted default identity files without copying private keys or
forwarding the agent or X11. Terminal output is parsed by xterm.js with
bounded buffering and scrollback. Guest escape sequences cannot set the
clipboard or open links.

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

## Remote desktop

The Ubuntu layer installs SHA-256-pinned KasmVNC 1.5.2. Its web listener
binds only to guest loopback. X11 accepts local Unix-socket connections
with an Xauthority cookie. A process-scoped seccomp filter blocks IPv4 and
IPv6 UDP sockets in KasmVNC before launch. Failure to install the filter
prevents KasmVNC from starting. Xfce applications use the existing VM
network policy. Box reserves guest port 8444 for the pinned SSH tunnel and
requires removal of an existing publish on that port before setup.

KasmVNC Basic auth is disabled. The guest display belongs to the same trust
boundary as the other processes in this single-user development VM. Any
process inside the guest that can reach its loopback may access the desktop.
SSH host-key verification and client authentication control access across
the guest boundary. The browser proxy authorizes local viewers independently,
so there are no display passwords to store or copy.

The local HTTP proxy binds an OS-assigned IPv4 loopback port and checks the
exact Host and WebSocket Origin. It exchanges a one-minute single-use link
for an HttpOnly SameSite=Strict cookie scoped to an unguessable path. It
strips browser cookies and authorization headers before proxying and strips
guest Set-Cookie headers on the return path. Keep the one-time link private
while it is valid. Closing the tunnel ends its browser session. The guest
session keeps running until its service or VM stops.

KasmVNC rejects clipboard transfer in both directions, and the browser
proxy sends a Permissions Policy disabling the Clipboard API in browsers
that enforce it.

### macOS guest viewer

The app hosts guest content in a [separate native WKWebView](desktop/desktop_view_darwin.m)
with a nonpersistent data store and its own content controller. This view
has no Wails scripts, message handlers or asset scheme. Its navigation
delegate allows main-frame navigation only to the exact loopback proxy
origin, including after redirects. It declines popups, downloads, file
pickers and media capture. The proxy adds a viewer Content Security Policy
that restricts network resources and frames. The proxy retains its
authorization, Host, Origin and `X-Frame-Options: DENY` checks.

The native viewer suppresses AppKit clipboard actions, and a document-start
script blocks Clipboard API writes, legacy editing commands and clipboard
and drag events before guest scripts run. This is separate from KasmVNC's
guest-side clipboard settings.

The trusted app toolbar offers one-shot plain-text transfer in either
direction. **Paste to VM** reads the Mac pasteboard on that action.
**Copy from VM** writes to the Mac pasteboard after a successful read from
the active VM. Each transfer uses a short-lived command channel on the
viewer's pinned SSH connection. The guest helper runs as the SSH user and
uses `xclip` with the Xfce display's Xauthority file. Clipboard text travels
as UTF-8 data on stdin or stdout, with a 1 MiB limit in each direction.
The app does not send clipboard text through Wails or evaluate it as code.
KasmVNC's clipboard channels and the native viewer's clipboard access stay
disabled. The app does not monitor either clipboard.

On macOS 27.2.1, the [native isolation fixture](desktop/scripts/verify-desktop-isolation.js)
used a real click to attempt a Wails binding, a post to the Wails
`external` message handler, a Wails runtime request, popup and external
navigation (direct and via redirect), and legacy and asynchronous clipboard
writes from the guest document and an attempted child frame. The checks
passed, the second origin received zero requests, and the host
pasteboard change count stayed the same. The
[bundle verification](desktop/scripts/verify-desktop.swift) checks the
same native view in the running app with a real VM.

### Linux guest viewer

The [GTK viewer](desktop/desktop_view_linux.c) has a dedicated WebKit web
context, user content manager, and ephemeral network session with its own
nonpersistent website data manager. It has no Wails asset scheme, runtime
scripts or message handlers. Native policy accepts navigation requests and
displayable main-frame responses on the exact loopback proxy origin,
including after redirects. The viewer connects directly to the loopback
listener. The proxy CSP disallows frames and cross-origin
resources. WebKitGTK's navigation action API does not expose an explicit
main-frame flag, so the frame restriction starts at the proxy CSP and is
also checked at the native response stage. Popups, downloads, file pickers,
context menus and permission requests are declined.

The viewer disables WebKitGTK JavaScript clipboard access and guards clipboard
methods, legacy editing commands, clipboard shortcuts, paste gestures, and
drag and drop at document start. KasmVNC's clipboard channels remain disabled.
The trusted toolbar uses GTK's standard clipboard and the same bounded,
pinned-SSH plain-text transfer as macOS. GTK text must be UTF-8 and cannot
contain embedded NULs.

WebKitGTK manages the separate Linux PRIMARY selection internally. The viewer
suppresses ordinary pointer text selection and middle-click paste. WebKitGTK
has no per-view switch for automatic publication of programmatically selected
text to PRIMARY. This is a guest-to-host selection limitation distinct from
the standard CLIPBOARD selection and explicit toolbar transfers.

The [Linux isolation fixture](desktop/scripts/verify-desktop-isolation-linux.js)
drives the native viewer with a real click. It checks absence of Wails bindings,
clipboard API denial, the standard host clipboard, popup and attachment
denial, direct and redirected navigation to an unrelated origin, and clear
cookies and local storage on reopening the same origin.

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

The optional [agent factory](factory/README.md) runs Codex with full guest
permissions. Its owner-file checks guard the supervisor's normal publication
path. The guest user can bypass them. Use a dedicated VM and scope its Git
and provider credentials to the work you intend to authorize.

- Log Claude Code and Codex in yourself, inside the VM. Those logins live there.
- Give the VM a fine-grained GitHub token scoped to the repositories it needs,
  never a personal token with full access.
- A host-side credential proxy that keeps tokens out of the VM is planned
  (phase 3 in the [design](docs/plan.md)).
- Images, when they exist (phase 2), will carry whatever was on the disk,
  logins included. Base images are built before anything is logged in.

Saved images and exported archives include the whole guest disk. They can
contain SSH credentials, API tokens, agent logins, local data and deleted data
still present in filesystem blocks. Save and export warn about this. Treat the
archives as sensitive files and only import archives you trust. SHA-256 verifies
disk integrity during archive import. Local `box image copy` uses the source
store's recorded disk digest, so only copy from a store you control. A VM
created from an image still uses the normal host isolation
and gets fresh SSH host keys and pins.

The [project image recipes](images/README.md) build signed-out guests and
check known credential paths and common plaintext token formats before saving.
The [smoke image](images/homebox-smoke/README.md) regenerates its PostgreSQL
TLS key when a new guest boots.
The scanner cannot inspect secrets inside compressed archives or arbitrary
binary formats. Review any additional data that goes into a distributable disk.

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

## Smoke-test image cache

The smoke runner loads container image archives from the Mac into the guest
over pinned SSH and saves misses over the same Mac-initiated connection. The
guest still cannot reach the Mac. The cache lives in a private host directory
and has no listener or shared mount. Its archives contain image data from the
scratch guest, so use it with trusted smoke VMs and images.
