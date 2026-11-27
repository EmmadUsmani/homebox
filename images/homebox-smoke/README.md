# Combined smoke image

`homebox-smoke` is a saved Ubuntu guest disk for Lima VZ on an Apple silicon
Mac. It includes the normal homebox profile, the Xfce remote desktop layer,
and the packages, runtimes, browser builds, container images, and test files
installed by the [smoke areas](../../smoke/README.md#areas). The optional News Digest
area uses a Git archive of a local checkout and generated fixture data.

## Recorded export (2026-11-26)

- Archive `~/homebox-images/homebox-smoke-20261126.hbimg.gz`
- Compressed size 11,953,853,371 bytes
- Archive SHA-256 `63ab781aebc936e23f21ce24358f034aba4dbf69d24deb187ba5963ebd4d99fb`
- Raw disk size 80 GiB (85,899,345,920 bytes)
- Raw disk SHA-256 `4fe3bcf06e12ba04950d7e04e8a1efc807c5828c54edc2d3817888662a7ce7af`

Before saving, the build checked Claude Code and Codex sign-in status, searched
the guest filesystem for `.env*` files and common credential file names, and
scanned home directories, system configuration, temporary files, container
storage, Nix and Tailscale state for OpenSSH private keys and common provider
token formats. Both agents were signed out. After removing public dependency
test keys, the scan found only the guest's generated SSH host keys.

## Verification

The original guest and a fresh restore each completed the seven-area suite
with 180 passes, zero failures, and one skip for unavailable hardware
performance counters. The restore passed `box doctor`, used new SSH pins, and
wrote journal entries under its new machine ID. The exact export was also
imported into a separate state directory with matching disk and EFI digests.

## Build it

Use an Apple silicon Mac with [the homebox host prerequisites](../../docs/host-setup-macos.md),
Go for building `box` (or set `BOX` to a compatible executable), and enough
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

To install a rebuild in the change tier’s default store, build with
`--export FILE` and run:

```sh
HOMEBOX_HOME="$HOME/.homebox" box image import FILE --name homebox-smoke
```

Import refuses to overwrite an existing image. To try a rebuild alongside
the default, use `scripts/test.sh change --image-file FILE --area os`.
You can also select a build’s isolated store with `--image-home DIR`.

## Load it and repeat the checks

On a compatible Mac, copy the export manually and import it into the intended
`HOMEBOX_HOME`. For a locally built image, set `HOMEBOX_HOME` to the state
directory printed by the builder and start at `box new`. If the builder
compiled the CLI, its executable is at `$HOMEBOX_HOME/box`. Use the image
name printed by the builder or by `box image import` when supplying `--image`.

```sh
box host init
box image import /path/to/homebox-smoke-YYYYMMDD.hbimg.gz
box new smoke-clone --image homebox-smoke
box doctor smoke-clone
box provision smoke-clone
SMOKE_NEWSDIGEST_REPO="$HOME/projects/news-digest" smoke/run.sh smoke-clone
```

The clone boots with a new hostname, machine ID, cloud-init instance ID,
SSH host keys and pins. Box restarts it once during setup so systemd and
the journal use the new identity. Ubuntu generates a fresh TLS key for
PostgreSQL on first boot. Smoke setup scripts reuse installed tools
and caches, including both Playwright browser revisions. The runner restages
the News Digest source from the checkout given for that run. If loading on another Mac, the original files and caches
keep their guest home directory and ownership. Run `box image ls` to see the
logical disk size and the SHA-256 of the raw disk.

The recorded November 26 export predates the browser sandbox profile update. The provisioning call
above brings it up to the current guest profile before running the current
suite. The verification totals above record the suite used to build and
verify this archive.
