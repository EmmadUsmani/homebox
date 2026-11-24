# Observability and the desktop app

The CLI (status, top, list and logs) and the macOS desktop app are built.
The remote desktop is available through the CLI and the macOS app. The
owner agreed this design on 2026-11-22. The decisions and what was turned down are also in
[decisions.md](decisions.md#observability-and-the-desktop-app). The plan places
it in phase 1b, before phase 2; [plan.md](plan.md) wins if the two disagree.

The goal: see what each VM is doing (whether it's running, healthy, how busy it
is, what it's serving and what it tried to reach) and work with it, from the
`box` command and from a desktop app, without running a server of our own.

## One core, two views

All of this is built once, in `internal/app`, and shown twice:

```
            ┌── internal/cli     (box status, box logs -f, --json)
internal/app┤
            └── desktop app      (Wails: Go backend, web frontend)
```

- `internal/app` gains read-only use cases that return plain data: `Status`,
  `Logs` (a stream), `Top`, and a stream of events (started, stopped,
  provisioning step, doctor result, network denial). The CLI prints them as a
  table or as JSON; the app draws them. Neither view holds any logic of its own.
- Behind the use cases sits a **source**: where the data comes from.
  - The *local source* reads this host: box's records, the driver, the host
    process and daemon logs, and the guest over box's own SSH.
  - The *SSH source* reads a VM on another host through the laptop's existing
    SSH login to that VM (see [other hosts](#vms-on-other-hosts)).

  Both return the same types, so the app's list of VMs and its detail screens
  don't care where a VM lives.
- The JSON that `box ... --json` prints is these same types. There is no
  separate format to keep in step.

## VMs on this host

What box can show, and where each fact comes from:

| What | Source |
|---|---|
| State: running, stopped, broken, provisioning (and which step) | box's records and the driver (`box ls` already does part of this) |
| CPU and memory used, seen from the host | the VM's host process (Apple's Virtualization process on macOS, using physical footprint for memory), with the Lima hostagent shown separately or labeled as the fallback |
| Load, memory, disk, top processes | a short read-only command in the guest, over SSH |
| Servers listening, and which are published on which host port | `ss` in the guest, matched against the VM's port forwards |
| Docker containers | `docker ps` in the guest |
| Coding agents running (`claude`, `codex`) and their sessions | the guest's process list |
| What the VM tried to reach and was refused | the network daemon's log, which already records denials |
| Isolation health | `box doctor`; the last result is kept with its time |
| Logs | see [live logs](#live-logs) |

### Commands

| Command | Does |
|---|---|
| `box ls [--json]` | Adds health, uptime and resource use to today's list |
| `box status NAME [--json]` | Everything in the table above for one VM |
| `box top [NAME]` | Live view of resource use, refreshed every few seconds |
| `box logs NAME [-f] [--source S] [--unit U] [-n N] [--json]` | Prints or follows logs (below) |

### Live logs

`box logs NAME -f` follows logs as they're written, like `tail -f`; the app
shows the same stream. Sources:

| Source | Where it lives | Notes |
|---|---|---|
| `provision` | box's record of the last provisioning run | Kept per run, so a failed step can be read later |
| `host` | the driver's host-process log (Lima's `ha.stderr.log`) | Boot, SSH readiness, port forwards |
| `net` | the network daemon's log | Denials and refusals; already rate-limited |
| `guest` | the guest's journal (`journalctl -f`), over SSH | Optionally one systemd unit, e.g. a dev server |

Following a host file has to survive rotation and truncation. Following the
guest keeps one SSH session open and reconnects if the VM restarts. With no
`--source`, box interleaves all of them, labelled and ordered by time.

The CLI reads the latest provisioning run and retains five on disk. A
snapshot takes up to `-n` lines per source (default 100, range 0–1000) and
sorts them by timestamp. Follow mode emits entries as they arrive. Box uses
the arrival time for lines without a readable timestamp. The guest journal
uses `--unit` for system units. During a disconnect, box reports that the
journal is unavailable and leaves host-side logs readable. Reconnection
resumes after the last valid journal cursor. Until box receives a valid
cursor, it requests the same bounded history and may repeat recent lines.
Box decodes bounded journal byte messages before stripping terminal controls.
Guest entries can include a syslog `identifier` in JSON. Plain output uses
that label when the unit is absent.

### Trust rules

These keep observability from weakening isolation:

- **The host pulls; the VM never pushes.** Everything from the guest comes over
  an SSH session that the host or laptop opens. Nothing in the VM connects to
  the host, and box adds no listener the VM could reach. A guest agent that
  reports to the host would open exactly the VM→host channel the isolation
  exists to block.
- **Guest data is untrusted.** A compromised VM can lie about itself, so box
  only displays guest-reported values and never acts on them. Box strips
  terminal escape sequences before printing and escapes HTML before the app
  renders anything. Sizes and timeouts are bounded, so a hostile guest can't
  hang or flood the viewer.
- **Read-only by default.** Status commands change nothing. The guest probe
  runs as the VM's user and needs no root.

## VMs on other hosts

There's no server of our own and no service on the LAN, so box sees VMs on
other machines only through what a laptop already has: its SSH login to each
VM, set up by the [laptop installer](../README.md). Call this level A; it's the
only level adopted. Two others were explored and turned down (below).

**Which VMs:** the hosts in `~/.ssh/homebox_config` (written by the installer),
plus any the user adds in the app. Add a host accepts an existing SSH alias
using letters, digits, dots, hyphens or underscores and starting with a letter
or digit. The VM's key must be pinned in `homebox_known_hosts`. The remote
Overview shows guest readings. For remote logs, the app directs users to
the VM's host Mac.

**What the app can tell about each one.** box checks each host on a timer and
reports what it found as a hint, not a diagnosis:

| What happened | Shown as | Likely meaning |
|---|---|---|
| The host name doesn't resolve, or the connection times out | Unreachable | The host is off, asleep or off this network |
| Connection refused | VM not running | The host is up, but box's SSH relay only listens while the VM's host process runs, so the VM is probably stopped |
| TCP connects but the SSH handshake fails | Connection broken | sshd in the VM is down or still booting |
| The host key doesn't match the pinned key | **Host key changed** | A warning, never a silent reconnect: the VM was rebuilt, or something is impersonating it (`box repin` on the host fixes the first case) |
| The login succeeds | Reachable | The same guest status as for a local VM, over this session |

**What level A can't do:** start, stop or create VMs on another host; show the
host's own resources; tell a stopped VM from a deleted one; or list VMs the
laptop has no SSH entry for. Those need box on the host, which only that host
can reach.

### Explored and not adopted

- **Level B: a status-only SSH login to the host.** The laptop would hold a
  second key that the host restricts to running `box remote --json`. Turned
  down (owner, 2026-11-22): it means a second login to set up per laptop and
  per host, and it turns on SSH to the host itself, which adds attack surface.
  The plan's separate "later" idea of a `box`-only SSH account for creating VMs
  remotely is unaffected by this decision and still unbuilt.
- **Level C: a box service on the LAN.** A host-side service that announces
  itself (Bonjour) and answers status requests. Turned down (owner,
  2026-11-22): it needs its own pairing and sign-in, it's a new network service
  with its own attack surface, and it's the most work. It would also have to
  be proven unreachable from every VM.

## The desktop app

A full desktop app (owner), built with **Wails**: a Go backend that calls
`internal/app` directly, and a web frontend in the OS's own web view. The same
code runs on the laptop and on a host; on a host it also sees the local VMs.

The local VM list and Overview refresh every few seconds, retaining readings
on refresh errors. The Overview memory card uses the guest's used and total
memory for the bar and shows the host process footprint separately. Local
lifecycle actions and doctor use the shared service.
Logs follow the CLI sources, support a guest-unit filter and retain the most
recent 5,000 lines while open. Leaving the view cancels the stream.

| Screen | What it does |
|---|---|
| VM list | Local VMs and remote ones (level A), with state, health and a reachability badge |
| VM detail | Status, resource use, published ports (open one in the browser), recent network denials, last doctor result; start, stop and doctor for local VMs |
| Logs | The live log stream, filterable by source |
| Terminal | Implemented with xterm.js and Go SSH. Each terminal tab opens a PTY on the VM’s shared pinned connection. |
| Files | Implemented. Browse from the SSH user’s home, show hidden files, and stream uploads and downloads through native file dialogs over the shared SSH connection. |
| Editor | Implemented. Open and edit up to 2 MiB of UTF-8 text with locally bundled Monaco. Save over SFTP with ⌘S and find with ⌘F. |
| Desktop | Connect to Xfce over pinned SSH, install the desktop layer if needed, and reconnect after SSH loss |

- **SSH in the app** obeys the same rules as box's SSH config: it uses only the
  pinned host keys from `homebox_known_hosts` (or box's local ones for VMs on
  this host), and it never forwards the SSH agent or X11. Keys come from the
  user's `~/.ssh` and are never copied anywhere.
- **Saving in the editor** refuses to overwrite a file that changed since it
  was opened (by size and modification time). **Reload and compare** displays
  the current VM text alongside your retained draft before you can save
  against that version. Saves stage a file alongside the original and replace
  it with a rename. See the [SFTP limits](../SECURITY.md#desktop-app).
- **Distribution: build from source.** No signed or notarized builds, so no
  Apple developer account (owner, 2026-11-22). An app built on your own Mac
  isn't quarantined, so it opens normally. See the
  [README](../README.md#desktop-app-macos) for the source build.
- **Wails version.** v3 reached stable on 2026-10-26. The app uses v3.1.0.
  See the [version and build checks](spikes/wails-desktop.md#version-and-build).
- **Layout.** The nested `desktop/` Go module imports the shared app service.
  Wails and the web view stay outside the root module and `box` builds with
  cgo disabled. See the [dependency check](spikes/wails-desktop.md#module-layout).

## Remote desktop (phase 2)

The opt-in Ubuntu layer runs Xfce with KasmVNC 1.5.2 at server-controlled
60 fps over WebSockets. The [display-server spike](spikes/remote-desktop.md)
compares latency, bandwidth and guest CPU. [Security](../SECURITY.md#remote-desktop)
describes the listener, SSH, browser and clipboard controls.

`box desktop NAME --install` adds the layer to an existing Ubuntu box.
`box desktop NAME` starts the guest session over pinned SSH and opens the
browser. `--no-open` prints the one-time URL. Closing the viewer or stopping
the local proxy leaves Xfce running until the guest or desktop service stops.
Guest startup failures go to the `homebox-desktop@<user>.service` journal.

The Desktop tab uses the same proxy and installer as the CLI. It leases
the app's pinned SSH connection for either a local VM or an SSH alias.
Choose **Connect** to open the viewer. The tab shows setup progress,
offers **Install desktop** if the layer is missing, and offers
**Reconnect** after a drop. Leaving the tab, selecting another VM or
closing the app releases the viewer and closes its loopback listener.
[Security](../SECURITY.md#macos-guest-viewer) describes the native WebKit
view and its isolation verification.

## Build order

1. **CLI.** `box status`, `box logs [-f]`, `box top`, a richer `box ls`, and
   `--json` on each. This settles the data types the app will use.
2. **Desktop app.** Local VMs, level A for remote ones, terminal, files and
   the light editor.
3. **Remote desktop.** With phase 2's desktop in the VM: `box desktop NAME` and
   the app's Desktop tab. Both are built.

## Desktop implementation decisions

- Check each SSH alias independently on launch, on window focus and when
  **Check again** is pressed. Reachable VMs refresh every 15 seconds.
  Failed checks back off from 15 seconds to at most 2 minutes. A successful
  check resets the interval.
- The app stores added aliases in
  `~/Library/Application Support/homebox/settings.json` with mode 0600 and
  atomic replacement. Window state can use the same file when implemented.
- The SSH status service uses `/usr/bin/ssh`. Native verification can point
  the app at copies via `HOMEBOX_DESKTOP_SSH_CONFIG`,
  `HOMEBOX_DESKTOP_SSH_KNOWN_HOSTS` and `HOMEBOX_DESKTOP_SETTINGS`.

## Open questions

- Whether `box doctor` runs on a schedule (and how often) or only on demand
  with its last result kept.
- Wails v2 or v3, and the module layout (above).
