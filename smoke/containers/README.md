# Containers

Run `smoke/run.sh NAME containers` from the Mac with Node.js available for the
host canary checks. Use a disposable Ubuntu VM with 2 CPUs, 6 GiB of memory,
room for images and a box port mapping for guest TCP 8073. The runner uses the
Docker Engine and Compose and Buildx plugins provided by the Ubuntu profile.
Setup adds Podman and its rootless helpers, QEMU user-mode emulation and binfmt
registration in the guest, checksum-verified kind and kubectl binaries, and
locked npm dependencies under the guest user's cache. Setup allocates
subordinate UID and GID ranges for rootless containers when the guest account
lacks them. The full cold run took about 10 minutes on a 2-CPU, 6-GiB
arm64 VM, excluding provisioning. The minimal-image builds add SDK downloads
and emulated compilation. Docker's build cache and installed tools remain
available for repeat runs.
A cold run can hit Docker Hub's anonymous pull quota. Docker, the standalone
BuildKit builder and Podman each have their own registry settings.

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
  with Testcontainers. The kind workload image is pulled with Docker and
  loaded into the cluster so it uses the guest's Docker registry settings.
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

## Minimal runtime images

The [fixtures](fixtures/minimal/) build in full images and copy the results into
the runtime images. Each run checks the loaded image architecture, the workload
output or HTTP response, a nonzero effective UID, and the absence of shells at
standard binary paths in the merged filesystem. The arm64 guest runs the suite
again as linux/amd64 using Docker's binfmt registration. Pure JavaScript, pure
Python, Java bytecode and the ASP.NET assemblies can be built on the guest's
native architecture. Static Go is cross-compiled. The Python extension wheels
and dynamically linked C++ and cgo binaries use target-platform build stages.

- Node.js 26 serves an escaped HTTP response using a locked npm dependency.
  The Mac repeats the request through box's port mapping after the Compose
  service releases guest port 8073. A temporary BusyBox sidecar joins the
  native Node container's PID and network namespaces, checks PID 1's
  command line and fetches the app over loopback.
- Python runs both a packaging version comparison and a MarkupSafe C extension.
- A Maven-built Java 21 jar and an ASP.NET 10 app on Ubuntu Noble Chiseled
  serve HTTP. The ASP.NET fixture uses invariant globalization for the
  chiseled runtime.
- Distroless cc loads a dynamically linked C++ binary using glibc and
  libstdc++. Distroless base runs Go code that calls libc through cgo.
- Distroless static and scratch make a verified HTTPS call with the same
  static Go program. Distroless supplies its own CA trust store. The scratch
  image copies the builder's CA bundle and declares a numeric nonroot user.
- Chainguard Node and Python reuse the JavaScript and pure-Python fixtures.
  Node uses the public `latest-slim` variant because `latest` includes a shell.
  Python uses its public `latest` runtime.

### Copying Python dependencies

Match the builder's Python minor version, distro and target architecture to
the runtime when installing native wheels. This fixture uses CPython 3.13 on
Debian 13 for both build and runtime, installs wheels into a directory with
`pip --target`, copies that directory into the final image, and sets
`PYTHONPATH` so the runtime interpreter can import from it. This also avoids
carrying virtualenv launcher paths across images. Import
`markupsafe._speedups` directly to prove the compiled module loads. A plain
`import markupsafe` can succeed through its pure-Python fallback. For an
extension that links other shared libraries, make those libraries available
in the final image as well.
