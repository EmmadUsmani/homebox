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
                   doctor, host init, status, top, logs); the only place that knows the order of steps
internal/vm        Driver interface and value types (Spec, Instance, Endpoint, ...)
internal/vm/lima   the Lima driver: writes Lima YAML, runs limactl
internal/guest     loads guest profiles (images + provisioning steps)
guest/             the profiles themselves, embedded in the binary:
  common/          distro-independent tool layer (POSIX sh)
  ubuntu/          Ubuntu LTS: image, apt packages, services
internal/state     box's own records (one file per VM) and port allocation
internal/egress    public-address policy, host interface checks and bounded daemon log
internal/confine   OS-specific sandbox profiles for the VM's host process and
                   network daemon (macOS: Seatbelt; Linux: later)
internal/vmnet     per-VM network daemon, IPv4 gateway and process monitoring
internal/relay     publishes the driver's loopback SSH port
internal/isolation app.Isolation: starts vmnet and confines the VM around it
internal/sshx      runs commands and scripts in a VM over the system ssh client;
                   reads guest public host keys and renders OpenSSH config and pins
internal/doctor    host-side controls and reporting for isolation checks
internal/hostinfo  host sizing, process liveness and macOS VM resource usage
internal/untrusted cleans text that came from a VM (escape sequences, control
                   characters) before box prints it
internal/execx     a small process-runner interface, so code that shells out
                   can be tested with a fake
e2e/               opt-in CLI lifecycle tests against a real VM
```

## Dependency rules

- CLI and desktop both call `app` and get the local service from
  `internal/wiring`. The desktop binds local status, logs and VM actions to the
  web view; its daemon executable is the sibling `box` inside the bundle.
  Desktop wiring supplies standard Homebrew and system tool paths on launch.
  The CLI retains its environment and supplies its own daemon executable.
  The desktop's [SSH source](../internal/app/remote.go) uses the laptop's
  OpenSSH configuration and shares the local probe's parser and limits.
- Only `internal/wiring` imports `internal/vm/lima`. The rest of box sees `vm.Driver`.
  Lima's YAML and `limactl` stay in `vm/lima`. The driver supplies paths
  needed by the rest of box, including its host-process log via `vm.Instance.HostLog`.
- [`desktop/session`](../desktop/session) owns a pinned SSH connection per
  active VM. Terminal tabs lease that connection and open independent PTY
  channels. The web view pulls bounded chunks and waits for xterm.js to
  consume each chunk before requesting more.
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
  lima/                 LIMA_HOME for the Lima driver, separate from any
                        Lima VMs the user runs themselves
```

The desktop stores added SSH aliases in its own
[`settings.json`](observability.md#desktop-implementation-decisions).
The laptop's SSH config and pins stay under `~/.ssh`.

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
   Host loopback and Lima's host DNS are disabled. Both processes retain
   file and execution restrictions. The daemon can additionally read
   the driver's exact host process PID file.
3. Ubuntu uses the second NIC for IPv4 egress and contacts public DNS
   resolvers directly. Provisioning removes the old proxy settings,
   redirects and Docker resolver override. The built-in NIC remains
   confined if guest root changes its routing. The socket daemon discards
   IPv6 Ethernet frames and creates IPv4 host sockets only.
4. The daemon opens the published SSH relay after observing the driver
   host process. On macOS it closes the listener and active forwards on
   a kqueue process-exit notification. `box stop` stops the guest before
   releasing the daemon.

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
- Lima's virtual gateway may answer ICMP echo within its userspace stack.
  Doctor uses TCP and UDP loopback canaries to test access to host services.

Box reads public SSH host keys over the driver's verified SSH connection
during start and provisioning. It stores them in the box record along with
the driver's identity path, and rebuilds the local OpenSSH files from those
records. Laptop exports use the same pins and the laptop's own identity.

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

## Testing

- `go test ./...` runs unit tests only; they need no VM, network or Lima.
- The macOS CI runner separately builds the desktop bundle. Native window
  automation runs in a signed-in macOS session with Accessibility and Screen
  Recording permissions. See the [spike](spikes/wails-desktop.md).
- `HOMEBOX_E2E=1 go test ./e2e/...` boots real VMs on this machine.
- `shellcheck` covers every script under `guest/`.
