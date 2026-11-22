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
internal/confine   OS-specific host confinement of the VM's network process
                   (macOS: Seatbelt; Linux: nftables later)
internal/sshx      runs commands and scripts in a VM over the system ssh client;
                   renders the laptop ~/.ssh/config snippet
internal/doctor    isolation checks run from inside a VM
internal/execx     a small process-runner interface, so code that shells out
                   can be tested with a fake
```

## Dependency rules

- `cli` → `app` → everything else. Nothing imports `cli` or `app` but `cmd/box`.
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

Everything box writes on the host lives under `~/.homebox` (`HOMEBOX_HOME`
overrides it):

```
~/.homebox/
  host.json             host settings (LAN publishing, laptop SSH keys)
  boxes/NAME/box.json   one VM's record: profile, size, index, ports
  boxes/NAME/...        per-VM runtime files (sandbox profile, proxy pid/log)
  lima/                 LIMA_HOME for the Lima driver, separate from any
                        Lima VMs the user runs themselves
```

## Ports

Each VM gets a small index `n` (1–99) when created:

- SSH: host port `2200+n` → guest port 22.
- HTTP: host ports `10000+100n … +99` → guest ports `8000 … 8099`.

Ports bind on `127.0.0.1` unless the host opted into LAN publishing
(`box host init --lan`), then on `0.0.0.0`.

## Testing

- `go test ./...` runs unit tests only; they need no VM, network or Lima.
- `HOMEBOX_E2E=1 go test ./e2e/...` boots real VMs on this machine.
- `shellcheck` covers every script under `guest/`.
