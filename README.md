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

umask 077
./box ssh-config > homebox_config &&
  ./box ssh-config --known-hosts > homebox_known_hosts
```

The default hostname is the Mac's mDNS name. Use `box ssh-config --host ADDR`
if the laptop needs a different address. Transfer both exported files from the
host to the laptop's Downloads folder over a trusted channel, such as a USB
drive. Then paste this into the laptop's terminal.

```sh
(
  set -eu
  umask 077
  ssh_dir="$HOME/.ssh"
  mkdir -p "$ssh_dir"
  test -s "$HOME/Downloads/homebox_config"
  test -s "$HOME/Downloads/homebox_known_hosts"
  install -m 600 "$HOME/Downloads/homebox_known_hosts" "$ssh_dir/homebox_known_hosts"
  install -m 600 "$HOME/Downloads/homebox_config" "$ssh_dir/homebox_config"

  include='Include ~/.ssh/homebox_config'
  config="$ssh_dir/config"
  if ! grep -Fqx "$include" "$config" 2>/dev/null; then
    new_config=$(mktemp "$ssh_dir/config.homebox.XXXXXX")
    trap 'rm -f "$new_config"' 0
    printf '%s\n' "$include" > "$new_config"
    if [ -f "$config" ]; then
      backup=$(mktemp "$ssh_dir/config.homebox-backup.XXXXXX")
      cp "$config" "$backup"
      cat "$config" >> "$new_config"
    fi
    mv "$new_config" "$config"
  fi
)
```

Run `ssh main`. OpenSSH checks the exported key on the first connection and
rejects a mismatch. Export and install both files again after adding or
recreating a VM. If an existing VM presents a different key set during start or
provision, box keeps the original pins and reports an error.

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
| `box ssh-config [--host ADDR \| --known-hosts \| --include]` | Prints laptop SSH aliases, their pinned keys, or the local Include line |
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

The VM has no mounts, and its sshd refuses SSH agent and X11 forwarding. Its network process on the
Mac runs under a Seatbelt sandbox that can reach only box's per-VM daemon on
loopback: an egress proxy that allows public addresses only, and a DNS
forwarder to public resolvers. In the guest, a transparent redirect sends all
TCP through that proxy. Root in the VM can remove the redirect, but that only
cuts off its own access: the rules that matter are on the host.

So the VM reaches the internet over TCP (and DNS), but not the Mac, its
loopback, your router or anything else on your network. ICMP (`ping`) and
other UDP don't leave the VM. Run `box doctor` to check.

Details: [architecture](docs/architecture.md), [to do](docs/todo.md), [phase 0 spike](docs/spikes/phase0-macos.md).

## Development

```sh
go test ./...                          # unit tests, no VM needed
HOMEBOX_E2E=1 go test ./e2e/...        # boots a real VM (several minutes)
shellcheck --shell=sh guest/*/*.sh
```

- [Design document](docs/plan.md): requirements, decisions and phases.
- [Code architecture](docs/architecture.md): packages and dependency rules.
- [Instructions for agents](AGENTS.md)
