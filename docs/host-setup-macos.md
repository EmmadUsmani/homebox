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
