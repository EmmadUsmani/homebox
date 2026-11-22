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

# Once: add the generated local SSH aliases to the top of ~/.ssh/config.
./box host init --ssh-include

# Create a VM (about 5 minutes the first time: image download + provisioning).
./box new main

ssh main
./box open main                      # open guest port 8000 in the Mac's browser
```

Use `main` with `scp`, `rsync`, or Remote-SSH in VS Code or Cursor. The local
alias uses the VM driver's existing identity. To add the Include yourself,
run `box ssh-config --include` and put its output at the top of
`~/.ssh/config`. The opt-in installer saves a copy of an existing config
before editing it.

## SSH from a laptop

On the VM host, authorize the laptop's public key and enable LAN publishing.
Restart an already running VM so it picks up those settings. Run
`box start NAME` for each older box that still needs its host keys pinned.

```sh
./box host init --lan --authorized-key ~/laptop_id_ed25519.pub
./box stop main
./box start main

./box ssh-config --installer > homebox-ssh.sh
```

The default hostname is the Mac's mDNS name. Add `--host ADDR` if the laptop
needs a different address. Transfer `homebox-ssh.sh` to the laptop over a
trusted channel such as AirDrop or USB, then run:

```sh
sh homebox-ssh.sh
ssh main
```

If the Mac has Remote Login enabled, you can generate and run the installer
in one step from the laptop. Replace the destination and binary path with
yours. This trusts the Mac's own SSH host key, which you should verify before
using the pipeline.

```sh
ssh user@mac.local '/absolute/path/to/box ssh-config --installer' | sh
```

The installer saves the aliases and pins under `~/.ssh` and prepends an
Include to `~/.ssh/config`, backing up the existing config before changing
it. OpenSSH checks the exported keys from the first VM connection. Generate
and run the installer again after adding or recreating a VM.

### After an intentional guest host-key change

If start or provision reports changed SSH keys, leave the VM running and
run `box repin NAME` on the Mac. Review the old and new SHA-256 fingerprints
and confirm only if you intended the change. Use `box repin NAME --yes` for
an intentional change in a non-interactive session. Box refreshes the Mac's
aliases, and you need to generate and run the laptop installer again.

The command reads new keys over the driver's authenticated connection. If
you also replaced Lima's own pinned guest key, restore that key before
running `box repin` so the driver can authenticate the guest.

## Commands

| Command | Does |
|---|---|
| `box host init [--lan/--no-lan] [--authorized-key FILE] [--ssh-include]` | Checks Lima, sets LAN publishing, adds laptop keys or installs the local SSH Include |
| `box new NAME [--profile ubuntu] [--cpus 4] [--memory 8] [--disk 50]` | Creates, starts and provisions a VM |
| `box ls` | Lists VMs with their SSH port and HTTP port range |
| `box start` / `stop` / `rm [-f]` NAME | Lifecycle |
| `box ssh NAME [-- cmd...]` | A shell or a command in the VM |
| `box ports NAME [--add G:H] [--rm G] [--set G:H]` | Shows or changes published ports (VM stopped) |
| `box provision NAME` | Re-runs provisioning (idempotent) |
| `box ssh-config [--host ADDR] [--installer \| --known-hosts \| --include]` | Prints laptop SSH aliases, their installer or pinned keys, or the local Include line |
| `box repin NAME [--yes]` | Reviews and replaces pins after an intentional guest host-key change |
| `box open NAME [GUEST_PORT] [--print]` | Opens the published URL on macOS or prints it. The guest port defaults to 8000 |
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

The VM has no mounts, and its sshd refuses SSH agent and X11 forwarding.
On macOS, Lima's built-in NIC carries inbound SSH and published HTTP ports
under Seatbelt confinement. A second NIC sends IPv4 traffic over a Unix
socket to box's per-VM gateway, which checks every new destination before
opening a host socket.

The gateway allows public IPv4 TCP, UDP and ICMP echo, including DNS directly
to public resolvers. It blocks host and private network destinations even
when root in the guest changes its routes. Run `box doctor` to check.

See the [architecture](docs/architecture.md) for known limitations.

## Development

```sh
go test ./...                          # unit tests, no VM needed
HOMEBOX_E2E=1 go test ./e2e/...        # boots a real VM (several minutes)
shellcheck --shell=sh guest/*/*.sh
```

- [Design document](docs/plan.md): requirements, decisions and phases.
- [Code architecture](docs/architecture.md): packages and dependency rules.
- [Alternatives](docs/alternatives.md): off-the-shelf options compared with homebox.
- [Instructions for agents](AGENTS.md)
