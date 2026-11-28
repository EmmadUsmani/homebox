# homebox

An exe.dev-style development VM on your own hardware. Run homebox on a Mac or a supported Linux host,
then SSH from your laptop straight into an isolated VM. In the Ubuntu guest you can
run coding agents such as Claude Code and Codex with full permissions, run any
server, and render and screenshot UI.

**Status:** The Mac host and the Linux CLI host are supported. See the
[design document](docs/plan.md) for the Windows host plans.

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

### macOS guests on Apple silicon

With Lima 2.3.0 and its packaged Darwin guest agent installed, create a
macOS Sequoia guest and connect over pinned SSH:

```sh
./box new mac --profile macos --disk 55
./box ssh mac
./box doctor mac
```

The first creation downloads Apple's roughly 16 GiB restore image. Start,
stop and remove macOS guests through `box`. The profile provides a macOS
admin environment with noninteractive sudo. The Ubuntu tooling,
saved-image workflows and `box desktop --install` are Ubuntu features.
See the [macOS guest security model](SECURITY.md#macos-guest-profile).

For iOS development, the [simulator spike](docs/spikes/macos-guests.md#ios-simulator)
used Xcode 16.2 and an 80 GiB guest disk.

## Quick start on Ubuntu Linux

Use an x86-64 Ubuntu host with KVM access, Landlock ABI 6 or later,
Lima 2.3.0, QEMU 10.2, OVMF, OpenSSH and iproute2. Install the distro
QEMU and OVMF packages so QEMU and qemu-img are in `/usr/bin`. The
[Ubuntu host setup](docs/host-setup-linux.md) has the installation script.
`box host init` checks host prerequisites. For KVM permission errors,
check membership in the `kvm` group and start a new login session after
changing it.

```sh
go build -o box ./cmd/box
./box host init
./box new main --cpus 2 --memory 4
./box ssh main
./box doctor main
```

SSH and published TCP ports bind to loopback by default. For laptop access,
use the LAN setup below. Use `box open --print main` to print a service URL
in a session without a browser. Start and stop through `box` so each boot
loads the Linux confinement policy. Save and restore use standalone QEMU disks on Linux.

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
| `box new NAME --image IMAGE [--cpus N] [--memory GiB] [--disk GiB]` | Creates a VM from a saved disk with fresh identity and ports |
| `box image save NAME IMAGE` | Saves a stopped VM's disk as a local image |
| `box image ls` / `box image rm IMAGE` | Lists or removes local images |
| `box image copy IMAGE --from-home DIR` | Copies a saved image from a trusted local state directory |
| `box image export IMAGE FILE` / `box image import FILE [--name NAME]` | Writes or verifies and loads a compressed image archive |
| `box ls [--json]` | Lists VMs, published ports, last doctor health and host-side resource use |
| `box status NAME [--json]` | Shows VM health, usage, listeners, containers, coding agents and recent egress denials |
| `box top [NAME] [--json]` | Refreshes resource use for running VMs every two seconds. Redirected output and `--json` use JSON Lines |
| `box start` / `stop` / `rm [-f]` NAME | Lifecycle |
| `box resize NAME [--cpus N] [--memory GiB] [--disk GiB]` | Shows the current size or resizes a stopped VM. The disk can only grow |
| `box ssh NAME [-- cmd...]` | A shell or a command in the VM |
| `box ports NAME [--add G:H] [--rm G] [--set G:H]` | Shows or changes published ports (VM stopped) |
| `box provision NAME` | Re-runs provisioning (idempotent) |
| `box logs NAME [-f] [--source S] [--unit U] [-n N] [--json]` | Prints recent logs or follows new entries |
| `box ssh-config [--host ADDR] [--installer \| --known-hosts \| --include]` | Prints laptop SSH aliases, their installer or pinned keys, or the local Include line |
| `box repin NAME [--yes]` | Reviews and replaces pins after an intentional guest host-key change |
| `box open NAME [GUEST_PORT] [--print]` | Opens the published URL on macOS or Linux, or prints it. The guest port defaults to 8000 |
| `box desktop NAME [--install \| --no-open]` | Installs the optional Ubuntu desktop or opens it over pinned SSH |
| `box doctor [NAME]` | Checks guest networking, host sandbox isolation and SSH listener ownership, then saves the result for `box status` and `box ls` |

Each VM gets an index `n`: SSH on host port `2200+n`, and guest ports
`8000–8099` on host ports `10000+100n …`. So for the first VM, a dev server on
guest port 8000 is `http://<mac>:10100` from the laptop.

For example, run `box status main` to see which guest listeners have a
published URL, or `box top main` to watch CPU, memory, load and disk usage.
On macOS, host CPU and physical memory footprint come from the Virtualization process.
`box status` also shows Lima's hostagent separately. If box cannot identify
the VM process, it labels the hostagent-only measurements. Guest resource
readings come from SSH.

## Saved VM images

On a supported macOS or Linux host, stop an Ubuntu VM and save its disk:

```sh
box stop main
box image save main dev-tools
box new work --image dev-tools
box doctor work
```

The new VM runs with its own SSH pins and port allocation. It defaults to the
image's disk size and accepts a larger `--disk`. CPU and memory follow the normal
`box new` defaults. Save and export warn because images carry everything on
the guest disk, including any agent logins and tokens.

To move an image to a host with the same backend and guest architecture, run `box image export dev-tools dev-tools.hbimg.gz`,
copy the archive by hand, then run `box image import dev-tools.hbimg.gz` there.
Import verifies the disk's SHA-256 and preserves the original image name.
See [image storage and compatibility](docs/images-and-sleep.md#images).

## Remote desktop

On the VM host, install Xfce and KasmVNC in a running Ubuntu box:

```sh
box desktop main --install
box desktop main
```

The installer also works on existing boxes. The second command starts Xfce
on demand and opens the browser on macOS or Linux. Use `--no-open` to print the
one-time URL and open it in a browser on the same machine within one minute.
Keep the command running while you use the viewer. Ctrl-C closes the
tunnel. You can run the command again to return to the same Xfce session
until the guest or its desktop service stops. Guest TCP port 8444 is
reserved for the SSH tunnel. Remove any existing published mapping for
that guest port before installing.

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

Ubuntu 26.04 LTS with build tools, Docker (with Compose and Buildx), Node,
Python and uv via mise, Claude Code, Codex, Playwright with Chromium, Xvfb
for headed apps, and Tailscale (installed, off). Log the agents in yourself;
for full permissions use `claude --dangerously-skip-permissions` or
`codex --dangerously-bypass-approvals-and-sandbox`.

Ubuntu provisioning allows unprivileged user namespaces for the Chromium
and Electron process sandboxes. Playwright callers can enable Chromium's
sandbox with `chromiumSandbox: true`. To apply the guest setting to an existing
VM such as `dev`, run `box provision NAME` or select **Re-run provisioning**
in the app. See [the guest security model](SECURITY.md#browser-processes-inside-the-ubuntu-guest).

## Stopping and starting

`box stop NAME` shuts the VM down cleanly; `box start NAME` boots it again
from the same disk, with no re-provisioning. Only `box rm` deletes a VM's disk.

Once stopped, use `box resize NAME` with the sizes you want to change. CPU and
memory changes follow the same host limits as `box new`. The root filesystem
grows at the next boot after a disk increase. If a resize is interrupted,
run `box resize NAME` without flags to reconcile and display the recorded size.

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

If the network daemon stops while a VM is running, `box status` and
`box doctor` report the failure. Save any work you can still access, then run
`box stop NAME` followed by `box start NAME` to restore networking.

## Isolation

The VM has no mounts, and its sshd refuses SSH agent and X11 forwarding.
Lima's built-in NIC carries inbound SSH and published HTTP ports.
On macOS the VM helper runs under Seatbelt. On Linux QEMU's built-in network
uses restricted SLIRP and its helper processes use Landlock and seccomp. A second NIC sends IPv4 traffic over a Unix
socket to box's per-VM gateway, which checks every new destination before
opening a host socket.

The gateway allows public IPv4 TCP, UDP and ICMP echo, including DNS directly
to public resolvers. It blocks host and private network destinations even
when root in the guest changes its routes. Run `box doctor` to check.

See [SECURITY.md](SECURITY.md) for the full security model, and the
[architecture](docs/architecture.md) for known limitations.

## Desktop app

For a local VM, use **Settings** to manage published ports, resize a
stopped VM, review changed SSH host keys, or copy the laptop SSH setup.
**About homebox** in the app menu shows the app and bundled `box` versions.
See [Desktop and CLI parity](docs/desktop-parity.md).

Build from the repository root with Go 1.27.4 and Node.js 22 (22.12 or
later), 24, or 26+. On macOS, install Xcode Command Line Tools and put Lima
in a standard Homebrew location (`/opt/homebrew/bin` or `/usr/local/bin`).

```sh
node desktop/scripts/build.js
open desktop/dist/homebox.app
```

On Ubuntu 26.04, install the [desktop build dependencies](docs/host-setup-linux.md#install-desktop-build-dependencies).
Build and run the application from a graphical session.

```sh
node desktop/scripts/build.js
desktop/dist/homebox-linux/homebox
```

Keep `homebox` and its bundled `box` together. To add a launcher, install the
generated entry into the current user's application menu.

```sh
install -Dm644 desktop/dist/homebox-linux/homebox.desktop "${XDG_DATA_HOME:-$HOME/.local/share}/applications/homebox.desktop"
```

The launcher points to this build directory. Reinstall the entry after
moving the build. Local Linux VMs require [Linux host support](docs/host-setup-linux.md).

Choose **Set up this Mac** or **Set up this host** when prompted, then
**New VM**.
The form starts with the same profile and sizes as `box new`, and the operation
panel shows provisioning steps and logs while you browse other VMs.
The app lists local VMs, including stopped ones, and shows their status,
resources, listening servers, network denials and live logs. Start, stop and
run doctor from the window. Use **More** on a local VM to re-run provisioning
or permanently delete the VM and its disks by typing its name.
**Open terminal** starts a pinned SSH session. The session tabs share a
connection and run separate shells. For remote VMs, the app offers SSH agent
keys first, then tries unencrypted default SSH keys. **Files** browses the
selected VM over SFTP, starting in the SSH user's home directory. Upload and
download use native file dialogs. Open UTF-8 text files up to 2 MiB for light
editing, use ⌘S and ⌘F on macOS or Ctrl+S and Ctrl+F on Linux to save
and find, and use **Reload and compare** if
the VM file changed since you opened it. Open **Desktop** and choose
**Connect** to use Xfce in the app. If the layer is missing, choose
**Install desktop** on a running Ubuntu VM. Leaving the tab or switching
VMs closes the tunnel. Reconnecting returns to the same Xfce session.
Use **Paste to VM** and **Copy from VM** in the connected Desktop toolbar
to transfer plain text, up to 1 MiB per click. After **Paste to VM**,
paste from the clipboard in the guest app. Run `box desktop NAME --install`
to add clipboard support to an existing desktop layer. Use **Files** to
move files between your computer and the VM.

The bundle embeds the frontend and includes the matching `box` executable for
the network daemon.
Other hosts lists VMs from the laptop SSH installer. Use **Add a host** for
another SSH alias pinned in `~/.ssh/homebox_known_hosts`. Reachable VMs show
live guest readings. A changed or missing pin blocks the connection. After an
intentional rebuild, review and replace the host keys in **Settings** on the
VM's host or run `box repin NAME` there. Then reinstall the
laptop SSH config.

See the [spike report](docs/spikes/wails-desktop.md) for the build background and
[SECURITY.md](SECURITY.md#linux-guest-viewer) for Linux viewer isolation and limits.

## Testing

Run the same command on either macOS host from a checkout with Lima, Go,
Node.js and ShellCheck installed. Desktop checks also need the Xcode command
line tools and a signed-in macOS session with Accessibility and Screen
Recording permissions. Close any running copy of the checkout’s desktop build
before starting the suite. Keep the verifier window in front while it sends
keyboard or mouse input. To run on the Mac mini, open the checkout there and
run the command locally.

- **Change**, before merging, targets ten minutes. Runs gofmt, Go vet and
  unit tests, the offline agent factory tests, and ShellCheck. It selects smoke areas from changes since the
  merge base with `origin/main`, including staged, unstaged and untracked
  files. Guest changes select OS checks and two checks for provisioned
  runtimes. CLI and core changes select `box doctor`. Desktop changes build
  the app and exercise native verifiers when Accessibility is available.
- **Batch**, every few merges, adds the real-VM Go tests, `box doctor` and every
  smoke area at quick level on a freshly provisioned guest. Targets 30–60
  minutes.
- **Full**, run occasionally, includes every smoke area at full level, the
  remote desktop host checks, and native verification of Files, Settings,
  the Desktop tab and viewer isolation. News Digest
  runs when `SMOKE_NEWSDIGEST_REPO` points to a checkout.

Run the same tiers on a supported x86-64 Linux host with KVM and Lima 2.3.0.
Install [the Linux host and contributor tools](docs/host-setup-linux.md) and
use an amd64 QEMU smoke image for saved-image runs.

```sh
scripts/test.sh change
scripts/test.sh change --base HEAD~1
scripts/test.sh batch --area os
scripts/test.sh full --area containers --match distroless
scripts/test.sh full --area gui --match 'GTK|Qt'
scripts/test.sh full --keep-vm
scripts/test.sh last
scripts/test.sh bisect GOOD BAD batch --area os
```

`--area` is repeatable and overrides automatic selection. Use a smoke area
name, `doctor`, `e2e` or `desktop`. Static checks run at every tier.
`--match` applies a case-insensitive regular expression to smoke labels.
`--keep-vm` prints the scratch state location and removal command. Pick an
unused index with `HOMEBOX_TEST_INDEX` (default 70) when running alongside
other VMs or test runners. The News Digest area runs when selected and
`SMOKE_NEWSDIGEST_REPO` points to a checkout.

The change tier uses `homebox-smoke` from `~/.homebox` when VM checks are
selected and guest profile files are unchanged. The runner clones the image
into isolated state and uses the store's recorded
disk digest. If cloning is unavailable, it warns and copies the disk.
Explicit archives are checked against their recorded digest during import.
Use a dedicated test image without signed-in accounts. See
[the build and import instructions](images/homebox-smoke/README.md).

Measured with `scripts/test.sh change --area os --area runtimes --area doctor`
from the default store on a Mac14,10. See the [run history](docs/test-runs.md).

| Staging method | Image staging | Total |
| --- | ---: | ---: |
| APFS clone | <0.1 s | 198.4 s |
| Previous gzip export and import | 483.0 s | 685.4 s |

| Set with | CLI equivalent | Purpose |
| --- | --- | --- |
| `HOMEBOX_SMOKE_IMAGE` | `--image NAME` | Choose the saved or imported image name. Default: `homebox-smoke`. |
| `HOMEBOX_SMOKE_IMAGE_HOME` | `--image-home DIR` | Choose a saved-image state directory. Default: `~/.homebox`. |
| `HOMEBOX_SMOKE_IMAGE_FILE` | `--image-file FILE` | Import an archive directly into isolated state under the chosen image name. Takes precedence over the store. |

Flags take precedence over their corresponding variables. Set
`HOMEBOX_SMOKE_IMAGE_HOME` if you previously used `HOMEBOX_HOME` to select
the source store. If the selected image is unavailable, the runner warns
and provisions a fresh VM for the change tier. Guest-profile changes use
fresh VMs in that tier. Batch and full default to fresh VMs. To test a
specific restored image with either tier, pass an image flag, such as
`scripts/test.sh full --image homebox-smoke`. These runs fail if the
image cannot be staged. GitHub CI remains informative.

Logs live in `.smoke-results/` by default. Set `SMOKE_RESULTS` to choose a
different directory. The runner prints and appends a row
to the [run history](docs/test-runs.md), including the image name, source
and disk SHA-256 for image boots. Commit the rows you want to share.
`last` shows the latest default and partial passes per tier. `bisect` checks candidates in
a temporary worktree using the selected tier and prints Git's first bad
commit. Use the same scope throughout the bisect.

Measured on the laptop with 2-CPU guests. Runs including containers
used 6 GiB of RAM and the others used 4 GiB. Times include static checks,
VM setup, selected tests and VM cleanup. The first two rows predate the
per-area quick split. The `change --base HEAD` run used guest and OS smoke
edits to verify area selection.

| Run | Smoke checks passed | Elapsed |
| --- | ---: | ---: |
| `scripts/test.sh change --base HEAD` | 18 | 5m 26s |
| `scripts/test.sh batch --area os` | 16 | 5m 50s |
| `scripts/test.sh change` | 79 | 8m 35s |
| `scripts/test.sh batch` with News Digest | 86 | 21m 39s |
| `scripts/test.sh full --area containers --match distroless` | 14 | 8m 44s |
| `scripts/test.sh full --area gui --match 'GTK\|Qt'` | 2 | 5m 44s |
| `scripts/test.sh change --area os --area runtimes --area doctor` from the default store | 20 | 10m 55s |
| `scripts/test.sh change --area os` with an archive override | 15 | 6m 30s |

With warm container archives, the batch finished below its 30–60 minute
target. The real-VM Go tests and fresh provisioning took 16m 36s combined.
The seven quick smoke areas took 4m 47s, including News Digest.

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
- [Mac isolation review](docs/security-review-2026-11-28.md): controls, coverage and limits.
- [Guest smoke tests](smoke/README.md): development checks with a Mac-side container image cache for repeat runs.
- [Local test runs](docs/test-runs.md): recorded results by tier.
- [Mac host setup](docs/host-setup-macos.md): keeping a Mac on and reachable (work in progress).
- [Ubuntu host setup](docs/host-setup-linux.md): installing host prerequisites and contributor tools.
- [Image build recipes](images/README.md) for building project images locally.
- [Images, sleep and resume](docs/images-and-sleep.md): how they work underneath, for the builders.
- [Agent logins](docs/agent-logins.md): signing agents in once per host instead of per VM (design, not built).
- [Agent factory](factory/README.md): running a bounded Codex build loop inside a guest.
- [Observability and the desktop app](docs/observability.md): status, live logs and the desktop app.
- [Desktop and CLI parity](docs/desktop-parity.md).
- [Desktop app look](docs/design/desktop-app.md): the approved screens and visual tokens.
- [Remote desktop spike](docs/spikes/remote-desktop.md) compares display servers over SSH.
- [macOS guest spike](docs/spikes/macos-guests.md) records the Lima boot and networking findings.
- [Wails desktop spike](docs/spikes/wails-desktop.md): version, build isolation and native GUI verification.
- [Alternatives](docs/alternatives.md): off-the-shelf options compared with homebox.
- [Instructions for agents](AGENTS.md)
