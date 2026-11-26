# Adding or changing a smoke area

Read [README.md](README.md), [run.sh](run.sh) and [lib.sh](lib.sh) first.

## Writing checks

- An area lives in `smoke/AREA/`: `setup.sh`, `test.sh`, optional `host.sh`,
  fixtures, and a `README.md` saying what it covers, what it installs, its
  known limits and roughly how long it takes.
- Test real behaviour, not versions: build, run, connect, query, render, and
  assert on the output. Give each check a specific label.
- Tests are deterministic, bounded with timeouts, clean up after themselves,
  and can run again on the same VM. Pin versions where practical.
- A check that can't pass without weakening the VM's isolation from the host
  or its network rules is a known limit: `skip` it with the reason and record
  it in the area's README. Never weaken isolation to make a check pass.

## Checks from another machine

Add `AREA/remote.sh` for a check that runs on a laptop over the LAN.
[`remote.sh`](remote.sh) runs it after the guest phases with `SMOKE_VM`
(SSH alias), `SMOKE_SSH_CONFIG` (optional config file), `SMOKE_LAN_HOST`
(effective SSH hostname), and `SMOKE_ARTIFACTS` (output directory). Keep
checks that need the VM host in `host.sh`. An area with `prepare.sh`
is skipped because its guest tests depend on the prepared inputs.

## Where fixes go

- Something nearly every development VM needs (a common package, an inotify
  limit, a locale, a PATH problem) is fixed in the guest profile
  (`guest/ubuntu/`, `guest/common/`, `guest/ubuntu/profile.yaml`) so new VMs
  have it from the first boot. Keep those edits small and additive.
- Heavier or niche tools are installed by the area's `setup.sh`, which also
  shows a user can install them.
- Fixes in Go code come with tests. Update the docs AGENTS.md lists.

## Running against a scratch VM

Use an isolated state dir and port range so the owner's VMs are untouched:

```sh
go build -o "$PWD/.bin/box" ./cmd/box
export HOMEBOX_HOME=~/.hb-smoke-AREA HOMEBOX_FIRST_INDEX=N BOX="$PWD/.bin/box"
$BOX host init
$BOX new smoke-AREA --cpus 2 --memory 4
smoke/run.sh smoke-AREA AREA
$BOX doctor smoke-AREA
```

Finish with a full run on a freshly created VM, so the guest-profile fixes are
proven from a clean boot. Stop VMs when idle, and remove the VM and its state
dir when done.

Before committing: `go build ./...`, `go vet ./...`, `go test ./...`, and
ShellCheck on every shell script you add or change.
