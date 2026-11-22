# homebox

An exe.dev-style development VM on your own hardware. Run homebox on a Mac,
then SSH from your laptop straight into an isolated Linux VM. In the VM you can
run coding agents such as Claude Code and Codex with full permissions, run any
server, and render and screenshot UI.

**Status:** phase 1 (Mac host) works end to end. Windows and Linux hosts come
later; see the [design document](docs/plan.md).

## Quick start (on the Mac that hosts the VMs)

```sh
brew install lima go
go build -o box ./cmd/box            # or: go install ./cmd/box

# Once: publish VM ports on the LAN and authorize your laptop's SSH key.
./box host init --lan --authorized-key ~/laptop_id_ed25519.pub

# Create a VM (about 5 minutes the first time: image download + provisioning).
./box new main

./box ssh main                       # a shell in the VM, from the host
./box ssh-config                     # paste the output into the laptop's ~/.ssh/config
```

Then, from the laptop: `ssh main`.

## Commands

| Command | Does |
|---|---|
| `box host init [--lan/--no-lan] [--authorized-key FILE]` | Checks Lima, sets LAN publishing, adds laptop keys |
| `box new NAME [--profile ubuntu] [--cpus 4] [--memory 8] [--disk 50]` | Creates, starts and provisions a VM |
| `box ls` | Lists VMs with their SSH port and HTTP port range |
| `box start` / `stop` / `rm [-f]` NAME | Lifecycle |
| `box ssh NAME [-- cmd...]` | A shell or a command in the VM |
| `box ports NAME [--add G:H] [--rm G] [--set G:H]` | Shows or changes published ports (VM stopped) |
| `box provision NAME` | Re-runs provisioning (idempotent) |
| `box ssh-config [--host ADDR]` | Prints `~/.ssh/config` entries for the laptop |
| `box doctor [NAME]` | Proves the VM reaches the internet but not the host or LAN |

Each VM gets an index `n`: SSH on host port `2200+n`, and guest ports
`8000–8099` on host ports `10000+100n …`. So for the first VM, a dev server on
guest port 8000 is `http://<mac>:10100` from the laptop.

## What's in the VM

Ubuntu 26.04 LTS with build tools, Docker (with Compose and Buildx), Node and
Python via mise, Claude Code, Codex, Playwright with Chromium, Xvfb for headed
apps, and Tailscale (installed, off). Log the agents in yourself; for full
permissions use `claude --dangerously-skip-permissions` or
`codex --dangerously-bypass-approvals-and-sandbox`.

## Isolation

The VM has no mounts and no SSH agent forwarding. Its network process on the
Mac runs under a Seatbelt sandbox that can reach only box's per-VM daemon on
loopback: an egress proxy that allows public addresses only, and a DNS
forwarder to public resolvers. In the guest, a transparent redirect sends all
TCP through that proxy. Root in the VM can remove the redirect, but that only
cuts off its own access: the rules that matter are on the host.

So the VM reaches the internet over TCP (and DNS), but not the Mac, its
loopback, your router or anything else on your network. ICMP (`ping`) and
other UDP don't leave the VM. Run `box doctor` to check.

Details: [architecture](docs/architecture.md), [phase 0 spike](docs/spikes/phase0-macos.md).

## Development

```sh
go test ./...                          # unit tests, no VM needed
HOMEBOX_E2E=1 go test ./e2e/...        # boots a real VM (several minutes)
shellcheck --shell=sh guest/*/*.sh
```

- [Design document](docs/plan.md): requirements, decisions and phases.
- [Code architecture](docs/architecture.md): packages and dependency rules.
- [Instructions for agents](AGENTS.md)
