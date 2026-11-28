# Combined smoke image

`homebox-smoke` is a saved Ubuntu guest disk for Lima VZ on Apple silicon macOS
or Lima QEMU on x86-64 Linux. It includes the normal homebox profile, the Xfce remote desktop layer,
and the packages, runtimes, browser builds, container images, and test files
installed by the [smoke areas](../../smoke/README.md#areas). The optional News Digest
area uses a Git archive of a local checkout and generated fixture data.

## Recorded macOS export (2026-11-26)

- Archive `~/homebox-images/homebox-smoke-check-20261126.hbimg.gz`
- Compressed size 18,594,706,851 bytes
- Archive SHA-256 `896e0921cba021c70057efb777d589cd7c47805829b021e16ef8bf769489a4ce`
- Raw disk size 80 GiB (85,899,345,920 bytes)
- Raw disk SHA-256 `8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0`

The verified archive was imported as `homebox-smoke` in the default
`~/.homebox` store. The build verified that both coding agents were signed
out and removed generated TLS keys and public test fixtures before scanning
for credentials. The guest's SSH host keys are regenerated on restore.

## Verification

The build ran all seven areas with 206 passes, zero failures, and one skip
for unavailable hardware performance counters. A fresh restore passed
`box doctor` and the runtimes checks.

## Recorded Linux image (2026-11-27)

Built on Ubuntu 26.04.1 LTS with Lima 2.3.0 and QEMU/KVM. The amd64
guest had 4 vCPUs, 8 GiB of RAM and an 80 GiB virtual disk.

- qcow2 disk size 36,172,791,808 bytes
- Disk SHA-256 `424f1ab36dfd5d96801e3929a4897fee8dfd82a628efb38de8ede13e55e7e508`
- Compressed export size 18,789,561,198 bytes
- Archive SHA-256 `f2db63a64de6f406fde294841bb465648cceb3fcd197f4ae5181cd2033902d7c`
- Build and export 38m 04s including the News Digest rerun

The seven smoke areas had 203 passes and two skips after the News Digest
integration rerun passed on the same build VM. The guest's hardware cycle
counter was unavailable and Ubuntu AppArmor blocked the bundled host
Chromium sandbox. The guest GUI checks, host gallery and SSH forwarding
refusal check still ran. Before saving, the build passed `box doctor` and
its credential scan and verified that both coding agents were signed out.
The verified export was imported as `homebox-smoke` in `~/.homebox`.

## Build it

Use an [Apple silicon Mac](../../docs/host-setup-macos.md) or an
[x86-64 Linux host with KVM](../../docs/host-setup-linux.md), Go for building
`box` (or set `BOX` to a compatible executable), and enough
CPU and memory for a 4-vCPU, 8 GiB guest. The VM has an 80 GiB logical disk.
Allow space for both the sparse VM disk and the saved image, plus at least
20 GB for an optional compressed export. Allow about an hour for provisioning,
downloads, smoke tests, hashing, and compression. Network and disk speeds
affect build time and package updates change the resulting bytes.

```sh
images/homebox-smoke/build.sh

# Optional export including the News Digest smoke area
mkdir -p "$HOME/homebox-images"
SMOKE_NEWSDIGEST_REPO="$HOME/projects/news-digest" \
    images/homebox-smoke/build.sh --export "$HOME/homebox-images/homebox-smoke-rebuilt.hbimg.gz"
```

Without `SMOKE_NEWSDIGEST_REPO`, the builder skips the News Digest area
and its installed dependencies. When set, the runner stages committed files
from the checkout's HEAD. Use a revision whose committed files are safe to
copy into the image.

The builder picks a free host port index and uses a fresh state directory
under your home directory. It installs the desktop, runs the smoke suite
and `box doctor`, verifies both agents are signed out, removes public test
keys and example credentials, scans for credential files and token formats,
and trims unused disk blocks before saving. The scan covers common plaintext
credentials and reports paths to investigate.

Use `--name IMAGE` to change the saved name. The builder leaves the local
image in its isolated state directory and removes the build VM by default.
Use `--keep-vm` to retain the VM for inspection. The builder stops it before
retaining it. Each run uses new state, and an existing export file causes an
error. Smoke logs go to `.smoke-results/` unless `SMOKE_RESULTS` is set.

To use a rebuild directly, pass the state directory printed by the builder:

```sh
scripts/test.sh change --image-home DIR --area os
```

The runner stages a copy-on-write clone on the same APFS volume, or warns
and copies if cloning is unavailable. For an owner-controlled local store,
it uses the recorded disk digest. Pass `--image-file FILE` to use an archive
and verify its disk digest during staging.

To install a rebuild in the default store, copy from the build's state directory:

```sh
HOMEBOX_HOME="$HOME/.homebox" box image copy homebox-smoke --from-home DIR
```

Copy refuses to overwrite an existing image.

## Load it and repeat the checks

On a host with the same backend and guest architecture, copy the export manually and import it into the intended
`HOMEBOX_HOME`. For a locally built image, set `HOMEBOX_HOME` to the state
directory printed by the builder and start at `box new`. If the builder
compiled the CLI, its executable is at `$HOMEBOX_HOME/box`. Use the image
name printed by the builder or by `box image import` when supplying `--image`.

```sh
box host init
box image import /path/to/homebox-smoke-YYYYMMDD.hbimg.gz
box new smoke-clone --image homebox-smoke
box doctor smoke-clone
SMOKE_NEWSDIGEST_REPO="$HOME/projects/news-digest" smoke/run.sh smoke-clone
```

The clone boots with a new hostname, machine ID, cloud-init instance ID,
SSH host keys and pins. Box restarts it once during setup so systemd and
the journal use the new identity. Ubuntu generates a fresh TLS key for
PostgreSQL on first boot. Smoke setup scripts reuse installed tools
and caches, including both Playwright browser revisions. The runner restages
the News Digest source from the checkout given for that run. If loading on another host, the original files and caches
keep their guest home directory and ownership. Run `box image ls` to see the
logical disk size and the SHA-256 of the saved disk file.

To apply newer guest-profile scripts to an older restored VM, run
`box provision smoke-clone` before running the smoke suite.
