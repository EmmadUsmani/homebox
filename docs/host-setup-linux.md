# Setting up an Ubuntu development host

Homebox runs Ubuntu development VMs on x86-64 Ubuntu hosts with KVM and
Landlock ABI 6 or later. The supported stack uses Lima 2.3.0 and QEMU 10.2
with Unix SLIRP forwarding. See [Linux host isolation](../SECURITY.md#the-vm-and-the-linux-host).

## Install host prerequisites

On an x86-64 Ubuntu machine, run this from the checkout as your regular user.
The script calls sudo for system changes and can be rerun.

```sh
./scripts/setup-ubuntu.sh --host-only
```

This installs CA certificates, curl, QEMU and its disk utilities, OVMF
firmware, nftables, the OpenSSH client, and Lima 2.3.0. The script checks
the Lima archive's SHA-256, prints package and tool versions, and adds your
user to the `kvm` group.

Enable hardware virtualization in firmware, then sign out and sign back in
to activate group membership. Check the installed binaries and KVM access
from the new session.

```sh
id -nG
limactl --version
qemu-system-x86_64 --version
test -r /dev/kvm && test -w /dev/kvm
```

## Run a VM

After installing Go from the contributor setup below, build and run the CLI.

```sh
go build -o box ./cmd/box
./box host init
./box new main --cpus 2 --memory 4
./box ssh main
./box doctor main
```

Use `box stop`, `box start` and `box rm` to manage the VM. The process
confinement is applied on each start. It uses Landlock and seccomp without
modifying the host firewall. SSH and published TCP ports bind to loopback
unless you enable `box host init --lan`. Install `xdg-open` to open
published URLs from a graphical session. A headless host can use
`box open --print` and `box desktop --no-open`.

## Install contributor tools

Run the same script without flags for the full development setup.

```sh
./scripts/setup-ubuntu.sh
```

In addition to the host prerequisites, it installs the Ubuntu build and CLI
packages listed in [the script](../scripts/setup-ubuntu.sh), including Git,
GitHub CLI and ShellCheck. It installs the exact Go version in
[`go.mod`](../go.mod) and verifies the archive's SHA-256. It uses mise for
Node 24 LTS, Python 3.14 and uv, and rustup for stable Rust and Cargo. It
installs Docker Engine, Buildx and Compose from Docker's apt repository.
The script prints package and tool versions and checks the Docker daemon
with sudo during installation.

Sign out and sign back in to activate the tool paths and membership in
`kvm` and `docker`. Membership in the `docker` group grants root-equivalent
access on the host.

Sign in to GitHub through the browser using HTTPS for Git operations. Set
your own global Git name and email before creating commits.

```sh
gh auth login --hostname github.com --git-protocol https --web
gh auth status
```

Verify the development tools from the new session. Compare `go version`
with `go.mod` and run the Docker smoke check as your regular user.

```sh
go version
node --version
python --version
uv --version
rustc --version
cargo --version
docker --version
docker run --rm hello-world
gh --version
git --version
shellcheck scripts/setup-ubuntu.sh
go vet ./...
go test ./...
```

## Run the smoke tiers

With KVM access, Go, Node.js and ShellCheck available, run
`scripts/test.sh change`, `scripts/test.sh batch`, or `scripts/test.sh full`
from the repository root. The runner uses disposable Linux VM state.
See [Testing](../README.md#testing) for tier selection, evidence and options,
and [the smoke image recipe](../images/homebox-smoke/README.md) for building
the local amd64 image used by the change tier.

## Install desktop build dependencies

The full contributor setup includes the GTK4, WebKitGTK 6.0 and X11
development libraries used by the Wails v3 desktop module, plus `xdg-open`
for published server links. To add these
dependencies to an existing Ubuntu 26.04 setup, run these commands.

```sh
sudo apt-get update
sudo apt-get install -y build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev xdg-utils
pkg-config --modversion gtk4 webkitgtk-6.0 x11
```

For the build and launcher, see [Desktop app](../README.md#desktop-app).
To drive the built app and capture its windows in a graphical Ubuntu session,
run the following from the repository root. The runner uses Xwayland,
`x11-utils`, `python3-gi`, `gir1.2-atspi-2.0` and `gir1.2-gtk-3.0`.

```sh
node desktop/scripts/verify-linux.js PATH_TO_EVIDENCE
```

Run the native isolation fixture on a disposable X11 display. It sets that
display's clipboard and uses XTest for a real click.

```sh
xvfb-run -a node desktop/scripts/verify-desktop-isolation-linux.js PATH_TO_EVIDENCE --replace-clipboard
```

The fixture uses `xvfb`, `xauth` and `libxtst6` in addition to the desktop
build dependencies.

For VM-backed GUI verification, use a disposable running VM and an Xvfb
display for reliable keyboard events. Pass the app executable and the VM name. The runner replaces
`homebox-linux-fixture.txt` in the guest's home directory.

```sh
box ssh NAME -- 'printf "linux editor fixture\n" > "$HOME/homebox-linux-fixture.txt"'
box desktop NAME --install
HOMEBOX_HOME=PATH_TO_TEST_STATE xvfb-run -a node desktop/scripts/verify-linux.js \
  PATH_TO_EVIDENCE desktop/dist/homebox-linux/homebox NAME
```

Set `HOMEBOX_VERIFY_LIFECYCLE=1` to stop and start the disposable VM.
Set `HOMEBOX_VERIFY_CLIPBOARD=1` on the disposable display to check clipboard
transfer in both directions. Use `GDK_DEBUG=no-portals` with
`HOMEBOX_VERIFY_NATIVE_DIALOGS=1` to exercise in-process GTK file choosers
instead of presenting a chooser on the host's live desktop.
