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
| `box ls [--json]` | Lists VMs, published ports, last doctor health and host-side resource use |
| `box status NAME [--json]` | Shows VM health, usage, listeners, containers, coding agents and recent egress denials |
| `box top [NAME] [--json]` | Refreshes resource use for running VMs every two seconds. Redirected output and `--json` use JSON Lines |
| `box start` / `stop` / `rm [-f]` NAME | Lifecycle |
| `box ssh NAME [-- cmd...]` | A shell or a command in the VM |
| `box ports NAME [--add G:H] [--rm G] [--set G:H]` | Shows or changes published ports (VM stopped) |
| `box provision NAME` | Re-runs provisioning (idempotent) |
| `box logs NAME [-f] [--source S] [--unit U] [-n N] [--json]` | Prints recent logs or follows new entries |
| `box ssh-config [--host ADDR] [--installer \| --known-hosts \| --include]` | Prints laptop SSH aliases, their installer or pinned keys, or the local Include line |
| `box repin NAME [--yes]` | Reviews and replaces pins after an intentional guest host-key change |
| `box open NAME [GUEST_PORT] [--print]` | Opens the published URL on macOS or prints it. The guest port defaults to 8000 |
| `box doctor [NAME]` | Checks isolation and saves the latest result for `box status` and `box ls` |

Each VM gets an index `n`: SSH on host port `2200+n`, and guest ports
`8000–8099` on host ports `10000+100n …`. So for the first VM, a dev server on
guest port 8000 is `http://<mac>:10100` from the laptop.

For example, run `box status main` to see which guest listeners have a
published URL, or `box top main` to watch CPU, memory, load and disk usage.
On macOS, host CPU and physical memory footprint come from the Virtualization process.
`box status` also shows Lima's hostagent separately. If box cannot identify
the VM process, it labels the hostagent-only measurements. Guest resource
readings come from SSH.

## Logs

```sh
box logs main
box logs main --source provision -n 20
box logs main -f --source guest --unit docker.service --json
```

The sources are `provision`, `host`, `net`, and `guest`. A snapshot prints
the latest 100 lines per source by default, ordered by time. Use `-f` to
keep receiving new lines and Ctrl-C to exit. `--unit` filters the guest
journal by systemd system unit. Plain output labels guest messages with their
unit or syslog identifier. `--json` emits one JSON object per line, with
`identifier` when the guest supplies one.

Host-side logs remain available when a VM is stopped. Box keeps the last five
provisioning logs and displays the latest one. Run `box provision NAME` on
existing Ubuntu VMs to enable guest journal access.

## What's in the VM

Ubuntu 26.04 LTS with build tools, Docker (with Compose and Buildx), Node and
Python via mise, Claude Code, Codex, Playwright with Chromium, Xvfb for headed
apps, and Tailscale (installed, off). Log the agents in yourself; for full
permissions use `claude --dangerously-skip-permissions` or
`codex --dangerously-bypass-approvals-and-sandbox`.

## Stopping and starting

`box stop NAME` shuts the VM down cleanly; `box start NAME` boots it again
from the same disk, with no re-provisioning. Only `box rm` deletes a VM's disk.

- **Kept:** files and git checkouts, installed packages and tools, agent
  logins, shell history, Docker images and volumes, the VM's ports and SSH
  aliases.
- **Lost:** running processes (dev servers, agent sessions, tmux, background
  jobs), and anything only in memory or `/tmp`.
- **Back on their own:** sshd and Docker start at boot, and so do containers
  with a restart policy (`--restart unless-stopped`, or `restart:` in Compose).
  To bring back your own server, run it as a systemd service or in such a
  container.

VMs don't start again on their own after the host restarts yet; run
`box start NAME`. Setting up an always-on host is in
[host setup](docs/host-setup-macos.md).

## Isolation

The VM has no mounts, and its sshd refuses SSH agent and X11 forwarding.
On macOS, Lima's built-in NIC carries inbound SSH and published HTTP ports
under Seatbelt confinement. A second NIC sends IPv4 traffic over a Unix
socket to box's per-VM gateway, which checks every new destination before
opening a host socket.

The gateway allows public IPv4 TCP, UDP and ICMP echo, including DNS directly
to public resolvers. It blocks host and private network destinations even
when root in the guest changes its routes. Run `box doctor` to check.

See [SECURITY.md](SECURITY.md) for the full security model, and the
[architecture](docs/architecture.md) for known limitations.

## Desktop app (macOS)

Build from the repository root with Go 1.27.4, Xcode Command Line Tools and
Node.js 22 (22.12 or later), 24, or 26+. For local VMs, install Lima in a
standard Homebrew location (`/opt/homebrew/bin` or `/usr/local/bin`).

```sh
node desktop/scripts/build.js
open desktop/dist/homebox.app
```

The app lists local VMs and shows their status, resources, listening servers,
network denials and live logs. Start, stop and run doctor from the window.
**Open terminal** starts a pinned SSH session. The session tabs share a
connection and run separate shells. For remote VMs, the app offers SSH agent
keys first, then tries unencrypted default SSH keys.
Create VMs with `box new NAME` in your terminal. The bundle embeds the frontend
and includes the matching `box` executable for the network daemon.
Other hosts lists VMs from the laptop SSH installer. Use **Add a host** for
another SSH alias pinned in `~/.ssh/homebox_known_hosts`. Reachable VMs show
live guest readings. A changed or missing pin blocks the connection. After an
intentional rebuild, run `box repin NAME` on the VM's host and reinstall the
laptop SSH config.

See the [spike report](docs/spikes/wails-desktop.md) for the build background.

## Development

```sh
go test ./...                          # unit tests, no VM needed
HOMEBOX_E2E=1 go test ./e2e/...        # boots a real VM (several minutes)
shellcheck --shell=sh guest/*/*.sh
```

- [Design document](docs/plan.md): requirements, decisions and phases.
- [Code architecture](docs/architecture.md): packages and dependency rules.
- [Decisions](docs/decisions.md): why each design decision was made, and what was turned down.
- [Security model](SECURITY.md): what homebox protects, how, and what it doesn't.
- [Mac host setup](docs/host-setup-macos.md): keeping a Mac on and reachable (work in progress).
- [Images, sleep and resume](docs/images-and-sleep.md): how they work underneath, for the builders.
- [Agent logins](docs/agent-logins.md): signing agents in once per host instead of per VM (design, not built).
- [Observability and the desktop app](docs/observability.md): status and live logs, plus the desktop app design.
- [Desktop app look](docs/design/desktop-app.md): the approved screens and visual tokens.
- [Wails desktop spike](docs/spikes/wails-desktop.md): version, build isolation and native GUI verification.
- [Alternatives](docs/alternatives.md): off-the-shelf options compared with homebox.
- [Instructions for agents](AGENTS.md)
