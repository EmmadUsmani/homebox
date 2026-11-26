# Guest smoke tests

Deeper checks than the provisioning self-test (`guest/common/selftest.sh`):
that a homebox VM works as a day-to-day development machine. Each directory is
one area:

| File | Runs | Purpose |
|---|---|---|
| `AREA/prepare.sh` | on the Mac, optional | Stages area inputs over SSH. Gets `SMOKE_VM` and `BOX`. Print a `SKIP` line and exit 77 to skip this area. |
| `AREA/setup.sh` | in the guest, as the VM user | Installs what the area needs. Idempotent. May use `sudo -n`. |
| `AREA/test.sh` | in the guest, as the VM user | Runs the checks with [lib.sh](lib.sh) (`PASS`/`FAIL`/`SKIP` lines); exits 1 on any failure. |
| `AREA/host.sh` | on the Mac, optional | Checks from outside the VM (published ports, the desktop proxy). Gets `SMOKE_VM`, `BOX`, and the run output directory in `SMOKE_ARTIFACTS`. |
| `AREA/README.md` | | What the area covers, what it installs, and known limits. |

Run against a VM on this Mac:

```sh
smoke/run.sh NAME            # every area
smoke/run.sh NAME runtimes   # one area
```

Logs go to `.smoke-results/` (ignored by git). To add an area, see [CONTRIBUTING.md](CONTRIBUTING.md).

Setup scripts install tools a user would install themselves; tools nearly every
VM needs belong in the guest profile (`guest/`) instead, so they're there from
the first boot.

## Where it has run

On November 26, 2026, all seven areas ran on an 8 GiB M2 Mac mini with
macOS 27.2.1 and Lima 2.3.0. A fresh Ubuntu ARM64 guest had 2 CPUs, 4 GiB
of RAM, LAN publishing and the optional desktop installed. The suite
reported 185 passes, zero failures and one skip for the unavailable
hardware `perf` cycle counter. The Mini reached the guest over pinned
SSH and exercised its published HTTP and WebSocket ports. The run also
verified that the guest and SSH service recovered after a stop and start.

## Areas

- [native](native/README.md): C and C++ toolchains, build systems, C libraries, debuggers and sanitizers, Go, Rust, Zig, WebAssembly, amd64 emulation, Homebrew, Nix and apt.
- [runtimes](runtimes/README.md): Node, Bun, Deno, Python with pip and uv, Rails, Java with Maven and Gradle, Kotlin, PHP, Elixir, .NET and the coding-agent CLIs.
- [services](services/README.md): databases, caches, queues and object storage; web servers, TLS, WebSocket, SSE, gRPC, GraphQL and WebRTC; outbound access and blocked private ranges; published ports from the Mac.
- [os](os/README.md): systemd services and timers, file watching, limits, disk, memory pressure, swap, locales, users and sudo, tmux, and state surviving a stop and start.
- [containers](containers/README.md): Docker across distros, BuildKit and amd64 emulation, Compose, volumes, limits, Podman, kind, Dev Containers, Testcontainers, and container isolation from the Mac.
- [gui](gui/README.md): GUI toolkits, OpenGL, Vulkan, media, fonts and audio, Playwright browsers, and apps driven through the remote desktop from the Mac.
- [newsdigest](newsdigest/README.md): a full-stack app (News Digest) built, tested, seeded and rendered in the VM and from the Mac; skipped unless `SMOKE_NEWSDIGEST_REPO` points at a checkout.
