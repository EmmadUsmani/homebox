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
To load or reproduce the full smoke VM, see [the combined image](IMAGE.md).

## From another machine on the LAN

Configure a pinned SSH alias for a running VM with LAN publishing, then run
the suite from the laptop. Use a dedicated smoke VM. The runner replaces the
guest's `~/homebox-smoke` directory on each run.

```sh
smoke/remote.sh -F ~/.ssh/smoke_config VM_ALIAS
smoke/remote.sh VM_ALIAS services
```

The runner uses ordinary SSH and accepts `SMOKE_SSH_CONFIG` as an alternative
to `-F`. It runs guest setup and tests, reports host-only phases as skips, and
skips areas that require host preparation, including News Digest. The services
area checks published HTTP and WebSocket traffic, Chromium rendering, guest
isolation from a laptop listener, and SSH and SCP timings. Install Node.js 22
or later and npm on the laptop for those checks. Set `SMOKE_LAN_PORT_BASE` to the
published host port for guest port 8000 if it differs from 12000. Guest port
8001 must map to the next host port.

Setup scripts install tools a user would install themselves; tools nearly every
VM needs belong in the guest profile (`guest/`) instead, so they're there from
the first boot.

## Container image cache

`run.sh` loads the image archives for the containers and services areas into
Docker before guest setup. The [containers](containers/images.txt) and
[services](services/images.txt) manifests list the references and platforms.
For an optional News Digest checkout, the runner reads that checkout's
`docker compose config --images` after staging its source.

The Mac stores archives under `${XDG_CACHE_HOME:-$HOME/.cache}/homebox/smoke-images/v1`.
Set `SMOKE_IMAGE_CACHE_DIR` to override the directory above `v1`. Keys include
the reference, including any pinned digest, and the platform set.
Docker pulls misses in the guest and retries rate-limit responses with backoff.
Cold runs can still hit Docker Hub limits. To authenticate pulls, run
`docker login` inside the scratch guest before running smoke. The Ubuntu
profile's separate first-boot self-test still pulls `hello-world`.

```sh
SMOKE_IMAGE_CACHE_REFRESH=1 smoke/run.sh NAME containers
smoke/cache.sh prune
```

Refresh pulls again and replaces each archive after a successful save. Prune
clears cache archives on the Mac. Both operations use the same cache directory
as the runner. The Mac transfers archives over pinned SSH. Podman imports its
Alpine base from the guest's Docker store, and Buildx uses Docker's built-in
builder so it can consume the same local platform variants. The containers
suite still needs public network access for package repositories.

## Where it has run

On November 26, 2026, all seven areas ran on an 8 GiB M2 Mac mini with
macOS 27.2.1 and Lima 2.3.0. A fresh Ubuntu ARM64 guest had 2 CPUs, 4 GiB
of RAM, LAN publishing and the optional desktop installed. The suite
reported 185 passes, zero failures and one skip for the unavailable
hardware `perf` cycle counter. The Mini reached the guest over pinned
SSH and exercised its published HTTP and WebSocket ports. The run also
verified that the guest and SSH service recovered after a stop and start.

A laptop then ran [the LAN suite](#from-another-machine-on-the-lan) against
the same VM over pinned SSH. The run reported 184 passes, zero failures and
six skips for host-only phases, News Digest preparation, and the unavailable
`perf` counter. The laptop reached the published guest web fixtures and
exercised the native app's [remote views](../docs/observability.md#remote-desktop-phase-2).

## Areas

- [native](native/README.md): C and C++ toolchains, build systems, C libraries, debuggers and sanitizers, Go, Rust, Zig, WebAssembly, amd64 emulation, Homebrew, Nix and apt.
- [runtimes](runtimes/README.md): Node, Bun, Deno, Python with pip and uv, Rails, Java with Maven and Gradle, Kotlin, PHP, Elixir, .NET and the coding-agent CLIs.
- [services](services/README.md): databases, caches, queues and object storage; web servers, TLS, WebSocket, SSE, gRPC, GraphQL and WebRTC; outbound access and blocked private ranges; published ports from the Mac.
- [os](os/README.md): systemd services and timers, file watching, limits, disk, memory pressure, swap, locales, users and sudo, tmux, and state surviving a stop and start.
- [containers](containers/README.md): Docker across distros, BuildKit and amd64 emulation, Compose, volumes, limits, Podman, kind, Dev Containers, Testcontainers, and container isolation from the Mac.
- [gui](gui/README.md): GUI toolkits, OpenGL, Vulkan, media, fonts and audio, Playwright browsers, and apps driven through the remote desktop from the Mac.
- [newsdigest](newsdigest/README.md): a full-stack app (News Digest) built, tested, seeded and rendered in the VM and from the Mac; skipped unless `SMOKE_NEWSDIGEST_REPO` points at a checkout.
