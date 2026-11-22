# homebox: code architecture

How the `box` code is laid out. [plan.md](plan.md) says *what* we build and why;
this file says where each piece lives and which way dependencies point. If the
two disagree, plan.md wins.

## Packages

```
cmd/box            main: wires concrete pieces together; no logic
internal/cli       commands, flags, output; calls app, nothing else
internal/app       use cases (new, start, stop, rm, ls, ssh, ports, ssh-config,
                   doctor, host init); the only place that knows the order of steps
internal/vm        Driver interface and value types (Spec, Instance, Endpoint, ...)
internal/vm/lima   the Lima driver: writes Lima YAML, runs limactl
internal/guest     loads guest profiles (images + provisioning steps)
guest/             the profiles themselves, embedded in the binary:
  common/          distro-independent tool layer (POSIX sh)
  ubuntu/          Ubuntu LTS: image, apt packages, services
internal/state     box's own records (one file per VM) and port allocation
internal/egress    public-address policy and host interface checks
internal/confine   OS-specific sandbox profiles for the VM's host process and
                   network daemon (macOS: Seatbelt; Linux: later)
internal/vmnet     per-VM network daemon, IPv4 gateway and process monitoring
internal/dnsfwd    forwards guest DNS queries to public resolvers
internal/relay     publishes the driver's loopback SSH port
internal/isolation app.Isolation: starts vmnet and confines the VM around it
internal/sshx      runs commands and scripts in a VM over the system ssh client;
                   reads guest public host keys and renders OpenSSH config and pins
internal/doctor    host-side controls and reporting for isolation checks
internal/execx     a small process-runner interface, so code that shells out
                   can be tested with a fake
e2e/               opt-in CLI lifecycle tests against a real VM
```

## Dependency rules

- `cli` → `app` → everything else. Nothing imports `cli` but `cmd/box`. `app`
  defines the interfaces it needs (ports); only `cmd/box` and the adapters that
  implement them (`internal/isolation`) import `app`.
- Only `cmd/box` imports `internal/vm/lima`. The rest of box sees `vm.Driver`.
  Lima's paths, YAML and `limactl` never leak out of `vm/lima`.
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
  ssh/config           local OpenSSH aliases (opt in via an Include)
  ssh/known_hosts      public host keys indexed by the aliases
  boxes/NAME/...        per-VM runtime files (sandbox profile, gateway socket, daemon PID and log)
  lima/                 LIMA_HOME for the Lima driver, separate from any
                        Lima VMs the user runs themselves
```

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
   count, connection attempts, idle time and denial logs.
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

## Testing

- `go test ./...` runs unit tests only; they need no VM, network or Lima.
- `HOMEBOX_E2E=1 go test ./e2e/...` boots real VMs on this machine.
- `shellcheck` covers every script under `guest/`.
