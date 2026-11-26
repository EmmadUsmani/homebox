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

- List each container image the area consumes in `images.txt`. Use
  `native` (the default), `amd64`, or `both` after the reference. Include the
  digest in the reference if the fixture pins it. Keep the manifest and build,
  Compose and test fixtures in sync.

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

## Levels and label filters

`smoke/run.sh` exports `SMOKE_LEVEL` and `SMOKE_MATCH` to all phases, including
guest setup and tests. The default level is `full`. Checks belong to both
levels unless wrapped in `full_only`, for example:

```sh
check 'Build and run hello' ./hello.sh
full_only check 'Build the optional cross-compiler' ./cross-compiler.sh
```

Use `full_only` for slow or heavier scenarios and for setup used only by those
scenarios. In `quick`, keep representative checks and the setup they need.

`SMOKE_MATCH` is a case-sensitive extended regular expression applied by
`check` and `check_sh` to the label before the command runs. Filtered checks
produce no result line. Setup and code outside these helpers still run.
Keep prerequisites outside filtered assertions, or select dependent checks
and their prerequisites in the same expression. A multi-check host harness
can use `smoke_matches 'Group label'` to decide whether to run the group.
