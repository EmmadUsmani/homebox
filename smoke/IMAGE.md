# Combined smoke image

`homebox-smoke` is a saved Ubuntu guest disk for Lima VZ on an Apple silicon
Mac. It includes the normal homebox profile, the Xfce remote desktop layer,
and the packages, runtimes, browser builds, container images, and test files
installed by the [seven smoke areas](README.md#areas). The News Digest area
uses a Git archive of a local checkout and generated fixture data.

## Local export

- Archive `~/homebox-images/homebox-smoke-20261126.hbimg.gz`
- Compressed size 11,953,853,371 bytes
- Archive SHA-256 `63ab781aebc936e23f21ce24358f034aba4dbf69d24deb187ba5963ebd4d99fb`
- Raw disk size 80 GiB (85,899,345,920 bytes)
- Raw disk SHA-256 `4fe3bcf06e12ba04950d7e04e8a1efc807c5828c54edc2d3817888662a7ce7af`

The local image is also available under `~/.hb-smoke-all/images/homebox-smoke/`.
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

## Recreate it

Run from the homebox checkout on the Mac. Use an isolated state directory and
unused port indices, such as the ones below. The full suite needs a committed
News Digest checkout and the [smoke test prerequisites](README.md).

```sh
mkdir -p .bin
go build -o .bin/box ./cmd/box
export BOX="$PWD/.bin/box"
export HOMEBOX_HOME=$(mktemp -d "$HOME/.hb-smoke-build.XXXXXX")
export HOMEBOX_FIRST_INDEX=80
export SMOKE_NEWSDIGEST_REPO="$HOME/projects/news-digest"

"$BOX" host init
"$BOX" new smoke-all --cpus 4 --memory 8 --disk 80
"$BOX" desktop smoke-all --install
smoke/run.sh smoke-all
"$BOX" doctor smoke-all
```

Use a fresh guest with no agent or service sign-ins. Review the disk for
credential files before saving it. Remove the public SSH key test fixtures
that the `ssh2` dependency installs in the npm and pnpm trees, their
corresponding pnpm cache entries, and the unreferenced Nixpkgs source
containing NixOS SSH test fixtures (`nix-store --delete`). The smoke runners
can fetch those fixtures again when needed. Run `sudo fstrim -av` in the guest
to discard free filesystem blocks and reduce storage used by the image.

```sh
"$BOX" stop smoke-all
"$BOX" image save smoke-all homebox-smoke
(umask 077; mkdir -p "$HOME/homebox-images")
"$BOX" image export homebox-smoke "$HOME/homebox-images/homebox-smoke-rebuilt-$(date +%Y%m%d-%H%M%S).hbimg.gz"
```

## Load it and repeat the checks

On a compatible Mac, copy the export manually and import it into the intended
`HOMEBOX_HOME`. If the image is already in that state directory, start at
`box new`.

```sh
box host init
box image import /path/to/homebox-smoke-YYYYMMDD.hbimg.gz
box new smoke-clone --image homebox-smoke --cpus 4 --memory 8
box doctor smoke-clone
box provision smoke-clone
SMOKE_NEWSDIGEST_REPO="$HOME/projects/news-digest" smoke/run.sh smoke-clone
```

The clone boots with a new hostname, machine ID, cloud-init instance ID,
SSH host keys and pins. Box restarts it once during setup so systemd and
the journal use the new identity. Smoke setup scripts reuse installed tools
and caches, including both Playwright browser revisions. The runner restages
the News Digest source from the checkout given for that run. If loading on another Mac, the original files and caches
keep their guest home directory and ownership. Run `box image ls` to see the
logical disk size and the SHA-256 of the raw disk.

The export predates the browser sandbox profile update. The provisioning call
above brings it up to the current guest profile before running the current
suite. The verification totals above record the suite used to build and
verify this archive.
