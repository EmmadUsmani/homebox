# homebox: code architecture

How the `box` code is laid out. [plan.md](plan.md) says *what* we build and why;
this file says where each piece lives and which way dependencies point. If the
two disagree, plan.md wins.

## Packages

```
cmd/box            CLI entry point and network daemon dispatch
desktop/           independent Go module with Wails and embedded React/Vite output
internal/wiring    assembles the local app.Service for both entry points
internal/cli       commands, flags, output; calls app, nothing else
internal/app       use cases (new, start, stop, rm, ls, ssh, ports, ssh-config,
                   doctor, host init, status, top, logs, desktop); the only place that knows the order of steps
internal/vm        Driver interface and value types (Spec, Instance, Endpoint, ...)
internal/vm/lima   the Lima driver: writes Lima YAML, runs limactl
internal/diskcopy clones or sparsely copies raw disks
internal/guest     loads guest profiles (images + provisioning steps)
guest/             the profiles themselves, embedded in the binary:
  common/          distro-independent tool layer (POSIX sh)
                   Node, Python and uv via mise
  ubuntu/          Ubuntu LTS: image, apt packages, services
internal/state     box's own records (one file per VM) and port allocation
internal/egress    public-address policy, host interface checks and bounded daemon log
internal/confine   OS-specific sandbox profiles for the VM's host process and
                   network daemon (macOS: Seatbelt; Linux: Landlock and seccomp)
internal/vmnet     per-VM network daemon, IPv4 gateway and process monitoring
internal/relay     publishes the driver's loopback SSH port
internal/isolation app.Isolation: starts vmnet and confines the VM around it
internal/sshx      runs commands and scripts in a VM over the system ssh client;
                   reads guest public host keys and renders OpenSSH config and pins;
                   opens a Go SSH transport using saved pins for the desktop CLI
internal/displayproxy  scopes HTTP and WebSockets to one authorized browser session
                       and dials the guest over its caller's pinned SSH transport
internal/doctor    host-side controls and reporting for isolation checks
internal/hostinfo  host sizing, process liveness and VM resource usage
internal/untrusted cleans text that came from a VM (escape sequences, control
                   characters) before box prints it
internal/execx     a small process-runner interface, so code that shells out
                   can be tested with a fake
e2e/               opt-in CLI lifecycle tests against a real VM
```

## Dependency rules

- On Ubuntu, the desktop module's Wails build uses GTK4, WebKitGTK 6.0
  and X11 headers. See the [desktop build dependencies](host-setup-linux.md#install-desktop-build-dependencies).
- CLI and desktop both call `app` and get the local service from
  `internal/wiring`. The desktop binds local status, logs and VM actions to the
  web view; its daemon executable is the sibling `box` inside the app bundle or Linux build directory.
  Desktop wiring supplies standard Homebrew and system tool paths on launch.
  The CLI retains its environment and supplies its own daemon executable.
  The desktop's [SSH source](../internal/app/remote.go) uses the laptop's
  OpenSSH configuration and shares the local probe's parser and limits.
- Only `internal/wiring` imports `internal/vm/lima`. The rest of box sees `vm.Driver`.
  Lima's YAML and `limactl` stay in `vm/lima`. The driver supplies paths
  needed by the rest of box, including its host-process log via `vm.Instance.HostLog`.
  `vm.Driver.Resize` changes a stopped VM's configured size, reported through
  `vm.Instance.Size`. [The application](../internal/app/resize.go) records the
  intended size before the driver edits its config. On interruption, a repeat
  `box resize NAME` reconciles that record against the driver's configured size.
  [The Lima driver](../internal/vm/lima/resize.go) stages its config edit before
  replacing the file. Lima grows the boot disk on start and Ubuntu cloud-init
  grows the root partition and filesystem.
- [`desktop/session`](../desktop/session) owns a pinned SSH connection per
  active VM. Terminal tabs lease that connection and open independent PTY
  channels. The web view pulls bounded chunks and waits for xterm.js to
  consume each chunk before requesting more.
- `box desktop --install` runs the embedded [Ubuntu desktop layer](../guest/ubuntu/desktop.sh)
  over pinned SSH. Systemd starts Xfce on demand and keeps it alive independently
  of viewer connections. The guest service owns its Xauthority file under
  `/run`. The optional layer includes `xclip` for the app's explicit clipboard
  transfers over pinned SSH. The host proxy keeps browser session credentials in memory.
  `internal/displayproxy` accepts a dialer for its fixed guest endpoint and
  owns the local listener and HTTP connections. Its caller owns SSH and
  cancels the proxy when SSH is lost. The app leases that connection from
  `desktop/session` and overlays a native WebKit view on the selected tab.
  WKWebView on macOS and WebKitGTK on Linux use independent native
  configuration and nonpersistent website data. The Linux view overlays the
  app's WebKitGTK widget within the existing GTK window. See the
  [macOS](../SECURITY.md#macos-guest-viewer) and
  [Linux](../SECURITY.md#linux-guest-viewer) viewer policies.
- `desktop/session` also leases a shared SFTP channel per SSH transport for
  the Files tab. Its bounded listings use pkg/sftp v2's directory handle
  API. The pinned upstream revision includes bounds on directory entry and
  extended attribute counts in protocol packets. The frontend lazy-loads the Monaco editor and bundled workers when a
  text file opens.
- Desktop dependencies stay in `desktop/go.mod`. The root module never
  imports the desktop module or Wails.
- Drivers know nothing about profiles, ports policy or egress. They get plain
  values (`vm.Spec`, `vm.StartOptions`).
- `guest` knows nothing about drivers beyond `vm.Image`.
- `egress` policy is pure (no I/O) and is the single source of truth for
  "is this address allowed".
- Code that shells out takes an `execx.Runner`, so unit tests use a fake.
  Tests that boot real VMs are opt-in (see Testing).

## Host state

Box stores its host state under `~/.homebox` (`HOMEBOX_HOME` overrides it).
The opt-in SSH installer adds an Include to `~/.ssh/config` and backs up the
original if it exists.

```
~/.homebox/
  host.json             host settings (LAN publishing, laptop SSH keys)
  boxes/NAME/box.json   one VM's record: profile, size, index, ports, SSH pins
  boxes/NAME/doctor.json latest doctor time, check counts and failing check names
  boxes/NAME/provision/ latest run (last.json) and five owner-only UTC-named logs
  ssh/config           local OpenSSH aliases (opt in via an Include)
  ssh/known_hosts      public host keys indexed by the aliases
  boxes/NAME/...        per-VM runtime files (sandbox profile, gateway socket, daemon PID and log)
  images/IMAGE/         saved raw disk and image.json metadata with VZ EFI variables
  lima/                 LIMA_HOME for the Lima driver, separate from any
                        Lima VMs the user runs themselves
```

The desktop stores added SSH aliases in its own
[`settings.json`](observability.md#desktop-implementation-decisions).
The laptop's SSH config and pins stay under `~/.ssh`.
The desktop app reads the same local records as the CLI. Its
[lifecycle service](../desktop/lifecycle.go) checks for saved host settings
and calls the shared app operations. It keeps a bounded tail of the active
operation's output in memory and reads step names from the provision record.

The [smoke runner's image archives](../smoke/README.md#container-image-cache)
live in the host's user cache, separate from box state and VM lifecycles.

## Ports

Each VM gets a small index `n` (1–99) when created. All its host ports derive
from `n`, so they stay the same across restarts:

| Port | Bound on | Serves |
|---|---|---|
| `2200+n` | `127.0.0.1`, or `0.0.0.0` with LAN publishing | SSH, via box's relay to the driver's SSH port |
| `10000+100n …` | same | HTTP: guest ports `8000 …`, one static driver forward each |
| `32200+n` | `127.0.0.1` | the driver's own SSH forward (internal) |

LAN publishing is a host setting (`box host init --lan`).

## Networking and isolation (macOS)

The [macOS networking spike](spikes/udp-macos.md) established the two-NIC
design. Lima's built-in NIC handles inbound SSH and static HTTP forwards.
The second NIC connects to box's IPv4 gateway over a Unix socket.

1. Box starts `box _vmnet NAME` under its own Seatbelt profile. The daemon
   binds an owner-only Unix socket in the VM's run directory and accepts
   one attachment. It provides DHCP and uses gVisor netstack to terminate
   guest TCP and UDP and handle IPv4 echo. Every new flow checks the numeric
   destination against the shared egress policy and the host's current
   interface networks before opening a host socket. The daemon bounds flow
   count, connection attempts, idle time and denial logs. Each VM has 1,536
   active flow slots and admits up to 500 new flows per second with a burst of
   1,536. DNS over UDP releases its slot after two seconds of inactivity.
   The daemon logs capacity and rate refusals.
2. Box starts Lima under a separate Seatbelt profile. Lima can connect to
   its own SSH forward, its runtime sockets and the gateway's exact Unix
   socket. It can bind only its SSH, HTTP forward and runtime sockets.
   Host loopback and Lima's host DNS are disabled.
   [Both profiles](../internal/confine/profile.go) default-deny file reads
   and Mach lookups. Lima can look up Virtualization.framework, the macOS
   directory helper, and the directory service's account-info and membership
   endpoints. The network daemon has no Mach lookup exceptions and can read
   the driver's exact PID file.
   Lima's `TMPDIR` is inside its own instance directory. System library
   paths include the dyld caches and exclude the APFS Data volume alias.
   [Doctor](../internal/doctor/sandbox_darwin.go) probes the installed
   profiles against host files and the notification service.
3. Ubuntu uses the second NIC for IPv4 egress and contacts public DNS
   resolvers directly. Provisioning removes the old proxy settings,
   redirects and Docker resolver override. The built-in NIC remains
   confined if guest root changes its routing. The socket daemon discards
   IPv6 Ethernet frames and creates IPv4 host sockets only.
4. The daemon opens the published SSH relay after observing the driver
   host process. It checks the listener and the watched process's kernel
   start time before and after each upstream dial. On macOS the check reads
   the host process's socket descriptors using `proc_info` inside Seatbelt.
   The Linux implementation matches the listener inode to descriptors in
   `/proc`. On macOS a kqueue process-exit notification closes the listener
   and active forwards. `box stop` stops the guest before releasing the daemon.

Known limitations ([to do](todo.md) tracks follow-ups):

- Public egress supports IPv4 TCP, UDP and ICMP echo. IPv6 and other IP
  protocols are outside this gateway's scope.
- A router with NAT reflection can reflect connections to its public IP
  back to a LAN port forward. Turn off NAT reflection on such routers.
- Restart the VM to reconnect its socket NIC after daemon failure. The
  built-in NIC cannot take over public egress.
- The daemon has outbound IPv4 access so it can enforce the destination
  policy. A compromise of the daemon could bypass that policy. Already
  established flows retain their admission decision until they close.
- The separately sandboxed Virtualization process runs the VM. The driver's
  host process can modify its own instance files, including disks and logs,
  and Lima's shared `networks.yaml`. Apple marks `sandbox-exec` deprecated.
  Apple can change service names and system paths across releases. The
  current allowlist was exercised on macOS 15.5 with Lima 2.3.0. On macOS
  27.2.1, Virtualization.framework reported a cache directory error after
  Seatbelt denied the directory helper lookup. The Lima profile includes the
  helper and group membership lookup for cache directory resolution. See the
  [scratch VM check](host-setup-macos.md#check-sandbox-compatibility).
- Lima's virtual gateway may answer ICMP echo within its userspace stack.
  Doctor uses TCP and UDP loopback canaries to test access to host services.

Box reads public SSH host keys over the driver's verified SSH connection
during start and provisioning. It stores them in the box record along with
the driver's identity path, and rebuilds the local OpenSSH files from those
records. Laptop exports use the same pins and the laptop's own identity.

## Networking and isolation (Linux)

Lima 2.3.0 prepares the cloud image before starting the VM. Box confines
`limactl start` and its descendants, and starts the gateway daemon under
its own profile. The QEMU binary override points to a box-owned launcher
that accepts Lima's capability probes and validates both network backends
before exec. The built-in SLIRP NIC retains its loopback SSH forward and
adds a Unix SSH forward for the relay. Only the socket NIC has outbound
access through the existing shared IPv4 policy.

Lima connects the gateway socket and passes its descriptor to QEMU. QEMU
can consequently deny `connect(2)` and datagram sockets in its own seccomp
profile. The gateway daemon verifies QEMU's Unix peer with `SO_PEERPIDFD`,
using the pidfs identity written at QEMU launch. It monitors that same
process with a pidfd. This avoids granting the daemon read access to other
processes' files in `/proc`. OpenSSH static forward sockets use the configured
loopback or LAN binding.

Landlock checks TCP ports. QEMU and its parent profile permit port zero
because libslirp reserves an ephemeral loopback source port for each
Unix-forwarded connection. Landlock ABI 6–8 scopes abstract Unix sockets
and signals. The Lima hostagent and gateway daemon use pathname Unix
sockets, whose destination paths those ABIs cannot constrain. The
hostagent's TCP connect rule limits the driver's SSH port, and normal
operation dials loopback. Landlock's TCP rule applies to any IPv4
destination on that port. The daemon needs IPv4 sockets to enforce the
gateway's destination policy. These are process-confinement limits if a
host helper is compromised. The QEMU process cannot initiate connections.

Linux CLI host support currently targets x86-64 Ubuntu, KVM, and QEMU 10.2
with Unix SLIRP forwarding. Saved disks, autostart, GPU passthrough and the
native desktop host application are separate work. The gateway's IPv4,
NAT reflection and existing-flow limits apply here as well.

## Status on the host

`box ls` reads host-side records and reports host process uptime. `box status`
and `box top` add guest readings, including guest uptime when available, by
running the bundled read-only [probe](../guest/common/status.sh) over SSH.

On macOS, box matches a Virtualization process's open disk descriptor to the
instance's exact disk path and file identity. It queries open files once per
sample and checks that each matched PID retains the same kernel start time
through the CPU and memory sample. The hostagent is identified by
its PID file and command line. Its measurements appear separately, or as a
labeled fallback when a Virtualization process cannot be identified. The
`host.memory_bytes` and `hostagent.memory_bytes` JSON fields report macOS
physical footprint from `proc_pid_rusage`. Each listener's `processes` field
contains the process names and PIDs available from the guest's `ss` output.

Linux reads QEMU and hostagent process statistics from procfs. The samples
report resident bytes and CPU use averaged over each process's lifetime.

## Desktop settings

The desktop [Settings service](../desktop/settings.go) calls the same port,
resize, SSH export and pinning operations as the CLI. Port and size edits
require a stopped VM. The app compares the reviewed fingerprints against
the current saved and detected fingerprints inside the shared re-pin lock.

## Testing

- `go test ./...` runs unit tests only; they need no VM, network or Lima.
- The macOS CI runner separately builds the desktop bundle. Native window
  automation runs in a signed-in macOS session with Accessibility and Screen
  Recording permissions. See the [spike](spikes/wails-desktop.md).
- `HOMEBOX_E2E=1 go test ./e2e/...` boots real VMs on this machine.
- CI runs ShellCheck on guest scripts. The [local test runner](../README.md#testing)
  checks shell scripts across the checkout, selects smoke areas by changed
  path, and keeps VM state and test logs separate from the regular box store.

## Guest agent factory

The optional [agent factory](../factory/README.md) runs in a Linux guest as
the SSH user. It uses the guest's Node.js, Git and Codex CLI. The supervisor
publishes to the project's existing origin and writes local status files that
the operator can read over SSH. The Ubuntu systemd user unit is an optional
launcher.
