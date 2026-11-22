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
internal/egress    egress policy (which addresses a VM may reach) and the
                   host-side proxy that enforces it
internal/confine   OS-specific sandbox profiles for the VM's host process and
                   network daemon (macOS: Seatbelt; Linux: later)
internal/vmnet     runs the per-VM network daemon and manages its process
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
  boxes/NAME/...        per-VM runtime files (sandbox profile, proxy pid/log)
  lima/                 LIMA_HOME for the Lima driver, separate from any
                        Lima VMs the user runs themselves
```

## Ports

Each VM gets a small index `n` (1–99) when created. All its host ports derive
from `n`, so they stay the same across restarts and the guest's proxy and DNS
settings can be written once, at provisioning:

| Port | Bound on | Serves |
|---|---|---|
| `2200+n` | `127.0.0.1`, or `0.0.0.0` with LAN publishing | SSH, via box's relay to the driver's SSH port |
| `10000+100n …` | same | HTTP: guest ports `8000 …`, one static driver forward each |
| `32200+n` | `127.0.0.1` | the driver's own SSH forward (internal) |
| `23000+n` | `127.0.0.1` | the VM's egress proxy (HTTP + CONNECT) |
| `23100+n` | `127.0.0.1` | the VM's DNS forwarder (UDP + TCP) |

LAN publishing is a host setting (`box host init --lan`).

## Networking and isolation (macOS)

From the [phase 0 spike](spikes/phase0-macos.md). Seatbelt can only tell
"localhost" from "anywhere", so it can't express "internet but not the LAN".
Instead the VM is confined to a few loopback ports, and box enforces the policy
behind them:

1. `box start` starts the VM's **host daemon** (`box _vmnet NAME`) under its
   own Seatbelt profile. It serves the egress proxy, public DNS forwarder and
   SSH relay, and can bind only those ports. It can read its binary, runtime
   files and the system paths it needs. It writes only in its run directory
   and cannot fork or execute programs after launch.
2. Box starts the driver under a separate Seatbelt profile with
   `sandbox-exec` (`vm.StartOptions.Wrap`). The driver's network process can
   connect only to its proxy, DNS and SSH ports on loopback, and bind only
   its SSH and published HTTP ports. In the home directory it can read only
   Lima's shared config (its SSH key), its own instance directory and its
   executable, so not other VMs' disks. Writes are limited to its instance
   directory (except `lima.yaml`, which the next start reads), Lima's network
   config and `/dev/null`. It can execute Lima, OpenSSH and the system helpers
   Lima needs at startup. Both profiles block LaunchServices, the pasteboard,
   AppleEvents and launchd job creation. `box doctor` uses the generated profile files to
   verify that both reject out-of-scope home reads and writes.
3. In the guest, provisioning points everything at `192.168.5.2` (the host's
   loopback as the VM sees it): proxy env and apt/Docker proxy config, DNS,
   and a transparent redirect (redsocks) so tools that ignore proxy settings
   still get out. Root in the guest can undo the guest side, but that only
   breaks its own access; the host side still holds.
4. `box stop` stops the VM, then its host daemon.

The VM layer is never trusted to enforce anything: the sandbox and the proxy
are both on the host.

Known limitations, accepted for now ([to do](todo.md) has the planned fixes):

- Only TCP (through the proxy) and DNS leave the VM; ICMP and other UDP don't.
- The proxy denies private ranges and every network attached to the host, but
  a router with NAT reflection could pass a connection to its own public IP
  back to a LAN port forward. Accepted: turn NAT reflection off on such routers.
- The network daemon still needs broad outbound network access to enforce
  the egress policy itself. A compromise of that daemon could bypass its
  address policy. The hostagent can still modify its own instance files
  (disks, logs) and Lima's `networks.yaml`, which only matters for
  Lima-managed networks that box never configures. Both profiles still allow
  other system services and reads outside the home directory. The VM itself
  runs in Apple's separately sandboxed Virtualization process.
  Apple marks `sandbox-exec` deprecated. It still works on the tested macOS
  host.
- The SSH relay forwards to the driver's loopback SSH port; if the VM is down
  and another program takes that port, the relay would forward to it. The
  generated local and laptop configs require the guest's pinned host keys
  even on the first connection.

Box reads public SSH host keys over the driver's verified SSH connection
during start and provisioning. It stores them in the box record along with
the driver's identity path, and rebuilds the local OpenSSH files from those
records. Laptop exports use the same pins and the laptop's own identity.

## Testing

- `go test ./...` runs unit tests only; they need no VM, network or Lima.
- `HOMEBOX_E2E=1 go test ./e2e/...` boots real VMs on this machine.
- `shellcheck` covers every script under `guest/`.
