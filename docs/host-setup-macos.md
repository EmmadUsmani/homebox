# Setting up a Mac as an always-on host

> **Work in progress.** Autostart isn't built yet, so a host restart still
> leaves its VMs stopped until you run `box start`. Update this page when
> autostart lands (see "Autostart" in [plan.md](plan.md) and the
> [to-do list](todo.md)), and check every setting below on a real host. Setting
> names are from recent macOS releases and may differ on yours.

How to set up a Mac, typically a Mac mini, as a homebox host that stays on and
reachable. For installing box and creating VMs, see the [README](../README.md).

## Hardware

- Apple Silicon. An 8 GiB M2 Mac mini has hosted a VM; more memory means larger
  or more VMs. New VMs size their CPUs and memory to the host.
- M3 or later is needed only for nested virtualization (Android emulators in a
  VM), which is a later phase.
- A wired network connection: steadier SSH than Wi-Fi, and needed for waking
  over the network.

## Keep it on

In **System Settings → Energy**:

- Turn on **Prevent automatic sleeping when the display is off**.
- Turn on **Start up automatically after a power failure**.
- Turn on **Wake for network access**.

Or from Terminal:

```sh
sudo pmset -a sleep 0 disksleep 0   # never sleep
sudo pmset -a autorestart 1          # start up after a power failure
sudo pmset -a womp 1                 # wake for network access
pmset -g                             # check the result
```

If the Mac does sleep, a running VM freezes and should carry on when the Mac
wakes (unverified with homebox).

## FileVault

Autostart needs the Mac to boot with nobody at the keyboard. FileVault stops
the boot until someone types the password, so hosts that use autostart turn it
off (**System Settings → Privacy & Security → FileVault**). The cost: the disk,
VM disks included, isn't encrypted if the Mac is stolen. See
[SECURITY.md](../SECURITY.md).

## Automatic login

*To be decided when autostart is built.* box's autostart job runs at boot before
anyone logs in. If Apple's Virtualization framework turns out to need a logged-in
user, the fallback is automatic login (**System Settings → Users & Groups →
Automatically log in as**), which works only with FileVault off.

## Autostart

*Not built yet.* Planned: `box autostart on` installs a boot-time launchd job
that starts every VM that was running when the Mac went down, with isolation
first. Until then, after a restart:

```sh
box start main
```

## Network and remote access

- `box host init --lan` publishes VM ports on the LAN; without it, only the Mac
  itself can reach them.
- The Mac's own SSH (**Remote Login**) isn't needed: you SSH into the VMs, not
  the Mac. Leave it off unless you use it for something else, such as the
  one-step laptop installer in the README.
- For access from outside your home network, Tailscale is planned (phase 2).

## Updates

- macOS updates can restart the Mac, which stops its VMs. Until autostart
  exists, install updates when you can start the VMs afterwards.
- Lima: `brew upgrade lima`, then restart the VMs.

## Check sandbox compatibility

From the checkout you want to test, run this block on the macOS host. It builds
box, provisions a 2 GiB VM using index 21, runs doctor before and after restart,
and prints Sandbox denials for `limactl` and `box` from the run. Use it when
upgrading macOS or Lima. The index must be free on this host. The scratch state
lives under your home directory and is removed when the check exits. If VM
removal fails, the block prints the scratch path and keeps it for recovery.
Review the log output for host paths and addresses before sharing it.

```sh
(
    set -eu
    scratch=$(mktemp -d "$HOME/.hb-sb-XXXXXX")
    export HOMEBOX_HOME="$scratch/state" HOMEBOX_FIRST_INDEX=21
    binary="$scratch/box"
    vm=sandbox-check
    started=$(date '+%Y-%m-%d %H:%M:%S')
    cleanup() {
        check_status=$?
        if [ -f "$HOMEBOX_HOME/boxes/$vm/box.json" ] &&
            ! "$binary" rm -f "$vm"; then
            printf 'Scratch state kept at %s\n' "$scratch" >&2
            check_status=1
        else
            rm -r -- "$scratch" || check_status=1
        fi
        /usr/bin/log show --start "$started" --style compact --predicate \
            'sender == "Sandbox" AND (eventMessage BEGINSWITH "Sandbox: limactl(" OR eventMessage BEGINSWITH "Sandbox: box(") AND eventMessage CONTAINS " deny("' ||
            check_status=1
        exit "$check_status"
    }
    trap cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    go build -o "$binary" ./cmd/box
    "$binary" host init
    "$binary" new "$vm" --cpus 2 --memory 2 --disk 20
    "$binary" doctor "$vm"
    "$binary" stop "$vm"
    "$binary" start "$vm"
    "$binary" doctor "$vm"
)
```

## Start VMs at host boot

FileVault hosts are outside the boot-time autostart design. On a supported
host, run `box autostart on` as the VM owner from a stable installation of
`box`, with Lima on your `PATH`. Review the generated plist in your box
state directory and run the printed `sudo /usr/bin/install …` command.
The root-owned plist puts the job in `/Library/LaunchDaemons`. Its
`UserName` runs Box as the owner.

```sh
box autostart status
```

The job runs at the next boot. To load a newly installed job in the current
session and check its log:

```sh
sudo /bin/launchctl bootstrap system "/Library/LaunchDaemons/com.homebox.autostart.$(id -u).plist"
cat "${HOMEBOX_HOME:-$HOME/.homebox}/autostart.log"
box ls
```

`box autostart off` prints the commands to unload and remove the daemon.
Use the same state directory when configuring or removing the job.
Re-run enable and reinstall the plist after moving Box or its tools.

Whether Virtualization.framework starts the VMs before login still needs
a Mac test. If it requires a graphical session, remove the LaunchDaemon
and run `box autostart on --login` as the owner. That installs a LaunchAgent
without sudo for the next graphical login. Check or remove it with
`box autostart status --login` or `box autostart off --login`.
Testing this fallback at host boot also requires automatic login.
