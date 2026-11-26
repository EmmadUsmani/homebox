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
smoke/run.sh NAME languages  # one area
```

Logs go to `.smoke-results/` (ignored by git). To add an area, see [CONTRIBUTING.md](CONTRIBUTING.md).

Setup scripts install tools a user would install themselves; tools nearly every
VM needs belong in the guest profile (`guest/`) instead, so they're there from
the first boot.

## Areas

<!-- One line per area: [name](name/README.md): what it covers. -->
