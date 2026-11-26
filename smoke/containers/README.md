# Containers

Run `smoke/run.sh NAME containers` from the Mac with Node.js available for the
host canary checks. Use a disposable Ubuntu VM with 2 CPUs, 6 GiB of memory,
room for images and a box port mapping for guest TCP 8073. The runner uses the
Docker Engine and Compose and Buildx plugins provided by the Ubuntu profile.
Setup adds Podman and its rootless helpers, QEMU user-mode emulation and binfmt
registration in the guest, checksum-verified kind and kubectl binaries, and
locked npm dependencies under the guest user's cache. Setup allocates
subordinate UID and GID ranges for rootless containers when the guest account
lacks them. Allow several minutes for cold pulls and package installs. Cached images and installed tools remain
available for repeat runs.

- Install and run jq with apt, apk, dnf, zypper and pacman across Debian,
  Ubuntu, Alpine, Fedora, Rocky Linux, openSUSE and Arch Linux. Arch uses the
  official amd64 image on an arm64 guest via QEMU. In that container, pacman
  uses `--disable-sandbox-syscalls` because its download seccomp filter fails
  under emulation. Docker confinement, package signatures and the host
  isolation policy still apply. BusyBox exercises a built-in applet and
  distroless uses its nonroot debug variant. [Pacman manual](https://man.archlinux.org/man/pacman.8.en)
- Build and load a multi-stage image with Buildx, prove a cache mount survives
  across separate build invocations, execute a build stage for amd64, and
  export both arm64 and amd64 artifacts from one build.
- Start a health-checked Compose HTTP service that queries Postgres and
  Redis by service name. Check named-volume persistence after recreation
  and reach the same fixture through box's published port from the Mac.
- Check writable bind and named mounts, a read-only mount, Docker logs,
  inspect, exec and stats, cgroup quotas, and an actual memory-limit kill.
- Build and run with rootless Podman, deploy and reach a kind workload,
  build and exec into a devcontainer, and run an HTTP integration test
  with Testcontainers. Kind loads the nginx image from the guest's Docker
  Engine, which also supplies it to Testcontainers.
- Reach a public HTTPS endpoint from a Docker bridge. Check representative
  private ranges and confirm a bridge container cannot reach temporary,
  locally verified HTTP canaries bound to Mac loopback and LAN interfaces.
  Run `box doctor NAME` separately for the router and host sandbox checks.

All container workloads run inside the guest. The Mac runs only short-lived
HTTP canaries for isolation checks, and the suite leaves host gateway and
firewall settings alone. The LAN canary check skips on Macs without a
non-loopback IPv4 interface. This area covers rootless Podman. Rootless Docker,
registry publishing, and inbound testing from another physical machine are
outside this area.
