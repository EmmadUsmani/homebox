# Ubuntu profile base image

`base-ubuntu` is the signed-out Ubuntu profile with the standard packages,
languages, coding agents and headless Chromium. The
[profile manifest](../../guest/ubuntu/profile.yaml) defines its contents.

With Lima VZ on macOS or Lima QEMU on Linux, initialize `box` and run:

```sh
BOX=box images/base-ubuntu/build.sh
box image ls
box new work
box doctor work
```

Use the `box` binary built from the checkout you want. The recipe uses the
active `HOMEBOX_HOME` store. It creates a throwaway VM, checks isolation and
scans for credentials before saving, and removes the build VM. The disk has
a 20 GiB logical size and uses sparse storage. Budget time for a complete
provisioning run and a read of the logical disk to compute its SHA-256.
See the [Mac timings](../../docs/images-and-sleep.md#macos-verification).

An ordinary `box new` builds this image on first use and rebuilds it after
changes to the profile's build inputs. Use `box image rebuild` to refresh
the cached packages on demand. See [base image selection and
compatibility](../../docs/images-and-sleep.md#images).
