# Setting up an Ubuntu development host

Homebox cannot run VMs on Linux hosts yet. Linux host support is in
development. See [current host support](../SECURITY.md#security-model).

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
