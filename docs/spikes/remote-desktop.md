# Remote desktop display-server spike

Benchmarked on 2026-11-22 using a scratch Ubuntu 26.04.1 arm64 VM with two
vCPUs and 4 GiB RAM, on an Apple M2 Pro running macOS 15.5. Xfce and a
1600×900 desktop were common to all three candidates. Chromium ran on the
same Mac and reached the VM over its pinned SSH connection. These are
same-machine results. LAN or Tailscale latency, packet loss, Safari and the
app's WKWebView still need testing.

## Setup and repeatability

The [spike installer](../../spikes/remote-desktop/desktop.sh) installs Xfce,
TigerVNC 1.15.0, noVNC 1.6.0 and websockify 0.13.0 from the Ubuntu archive,
plus KasmVNC 1.5.2 from a SHA-256-pinned GitHub release asset. Use a disposable
guest and set `HOMEBOX_USER` to its SSH user before running the installer.
Install Selkies 2.2.1 in a Python virtual environment with
`pip install selkies==2.2.1`. The measured build used a CPython 3.14 arm64
wheel. [KasmVNC release](https://github.com/kasmtech/KasmVNC/releases/tag/v1.5.2)
and [Selkies release](https://pypi.org/project/selkies/2.2.1/).

- TigerVNC ran at display `:1` with `-localhost yes -SecurityTypes None
  -geometry 1600x900` and an Xfce startup script. websockify served
  `/usr/share/novnc` on guest loopback TCP 6080 and proxied to TigerVNC
  on loopback TCP 5901. The client URL was
  `/vnc.html?autoconnect=1&resize=scale`.
- KasmVNC ran at display `:2` with `kasmvncserver :2 -disableBasicAuth`.
  In `~/.vnc/kasmvnc.yaml`, set `network.interface` to guest loopback,
  `network.websocket_port` to 8444, `network.ssl.require_ssl` to false,
  and `desktop.resolution` to 1600×900 with `desktop.allow_resize` false.
  The client URL was `/?autoconnect=1&enable_webrtc=false`. See the UDP
  bind caveat below before reproducing this setup.
- Selkies ran against Xfce on `Xvfb :3 -screen 0 1600x900x24 -nolisten tcp`,
  with `DISPLAY=:3` and `--addr` set to guest loopback, `--port 8082
  --use-cpu true --enable-basic-auth=false --audio-enabled false
  --microphone-enabled false --enable-clipboard false --gamepad-enabled false
  --manual-resolution true --manual-width 1600 --manual-height 900`. The
  client URL was `/`. Selkies 2.2.1 defaults to **WebSockets**. Guest logs
  confirmed software H.264 (x264), 60 fps and a configured 8 Mbps bitrate.
  The WebRTC transport was outside the SSH-only comparison.

Use the same Xfce startup script on each display, with
`exec dbus-launch --exit-with-session startxfce4` after unsetting
`SESSION_MANAGER` and `DBUS_SESSION_BUS_ADDRESS`. Put a 60-second, 1280×720/30 H.264
test clip at `/tmp/hb-test.mp4`. The exact synthetic clip from this run is
`hb-test.mp4` in the local evidence directory below (SHA-256
`0daae6f16043c0d80755e7dd98cba25775816dd62cdcf7c08f5737cc9ef6bcca`).

Run `npm ci` and `npx playwright install chromium` from the
[harness directory](../../spikes/remote-desktop/harness). Forward the relevant
guest loopback TCP port to a Mac loopback port with pinned OpenSSH
(`ssh -F <config> -o ExitOnForwardFailure=yes -N -L <bind>:<port>:<guest-loopback>:<port> <alias>`).
From a private output directory, run the harness once per backend:

```sh
node <repo>/spikes/remote-desktop/harness/measure.js \
  --name <backend> --target <port> --path '<client-path>' \
  --guest 'box ssh <vm> --' --display <display-number> --out <backend>.json
```

The harness writes `<backend>-desktop.png`, `<backend>-scroll.png` and
`<backend>-video.png` beside the JSON. This run kept them locally in
`~/Downloads/homebox-remote-desktop-spike/` under the prefixes `novnc`,
`kasm`, `kasm-tuned` and `selkies`. Screenshots and raw run data are local evidence.

## Method

- **Latency.** Press `x` 60 times with a randomized pause of 250–350 ms.
  A maximized Xfce terminal toggles a six-cell high-contrast patch per
  keypress. The harness polls a small composited browser crop via CDP
  and checks the expected brightness transition at calibrated pixels.
  A status animation or unrelated video frame cannot satisfy that check
  unless it changes those pixels in the expected direction. Time runs from
  key dispatch to receipt of the first matching screenshot. Screenshot
  polling adds capture and compositor scheduling time, so these are
  observed upper bounds on visible response in this setup.
- **Method validation.** On noVNC, direct canvas polling of the same
  target gave **47.4 ms median / 62.2 ms p95** with 60 observed events.
  Composited captures gave **62.1 / 70.5 ms**, about one display frame
  higher at the median. An earlier natural-glyph canvas run measured
  **29.2 / 31.4 ms** with a different stimulus and cadence. The screenshot
  method did not reproduce that absolute baseline. Canvas access is
  unavailable for Selkies' worker-drawn display. A content-aware CDP
  screencast pilot gave **75.2 ms** on noVNC, so it was dropped in favour
  of clipped screenshot polling. Chromium's DevTools capture starts
  with a [100 ms minimum capture period by default](https://chromium.googlesource.com/chromium/src/+/2985a2476a50ee04dbd78b6a0c3228c5f96a2559/content/browser/devtools/devtools_video_consumer.cc).
- **Traffic and CPU.** After each scene settles for three seconds, measure 20
  seconds of idle, a full terminal scrolling GPL text paced at 40,000 bytes/s,
  and looping 1280×720/30 H.264 test video in mpv. A loopback TCP proxy
  counts web traffic each way before SSH framing. Guest CPU is the percentage
  of total vCPU time busy from `/proc/stat`, including Xfce, workloads and
  SSH. The screenshot loop is stopped during these scenes. No UDP display
  data passed through the proxy. Reported zero traffic rounds to 0.0 KiB/s.

## Results

Each backend completed 60 keypresses with zero misses. The scene columns
show download/upload in KiB/s, followed by total guest CPU.

| Backend | Median / p95 (ms) | Idle down/up · CPU | Scroll down/up · CPU | 720p video down/up · CPU |
| --- | ---: | ---: | ---: | ---: |
| TigerVNC + noVNC | 62.1 / 70.5 | 0.0/0.0 · 0.8% | 3093.3/3.0 · 10.4% | 3146.8/1.9 · 17.7% |
| KasmVNC defaults | 75.2 / 99.3 | 0.0/0.0 · 1.0% | 1993.5/0.0 · 11.9% | 1972.4/0.0 · 21.9% |
| KasmVNC server-controlled 60 fps | 67.0 / 75.3 | 0.0/0.0 · 0.4% | 712.1/0.0 · 13.2% | 1374.8/0.0 · 25.7% |
| Selkies (WebSocket) | 47.9 / 77.4 | 11.3/0.6 · 22.0% | 810.5/0.6 · 24.4% | 744.5/0.6 · 28.9% |

The TigerVNC video and default KasmVNC motion rates reproduced the first pass
closely. TigerVNC scrolling dropped from 3920.5 to 3093.3 KiB/s in the
rerun. Selkies scrolling stayed near 0.8 MiB/s and idle CPU reproduced at
22%, while its idle traffic was 11.3 KiB/s down compared with roughly
20 KiB/s in the handoff. The still screenshots verified that each
workload reached the client. They do not quantify motion image quality.

## KasmVNC latency and networking

The first pass used direct canvas polling of a natural typing target. Each
row had 60 observed keystrokes. Traffic columns show down/up in KiB/s.
All first-pass idle traffic rounded to zero.

| First-pass configuration | Median / p95 (ms) | Scroll down/up | Video down/up | Guest CPU idle / scroll / video |
| --- | ---: | ---: | ---: | ---: |
| TigerVNC + noVNC | 29.2 / 31.4 | 3920.5 / 3.1 | 3148.3 / 1.8 | 0.6% / 10.4% / 17.0% |
| KasmVNC defaults | 96.4 / 112.6 | 1978.9 / 0.0 | 1972.8 / 0.0 | 0.6% / 11.1% / 21.3% |
| KasmVNC framebuffer comparison off | 95.7 / 111.2 | 1986.0 / 0.0 | 1960.6 / 0.0 | 0.2% / 11.2% / 21.1% |
| KasmVNC fixed quality | 96.5 / 112.7 | 1977.8 / 0.0 | 1961.9 / 0.0 | 0.3% / 11.3% / 21.5% |
| KasmVNC generic client `framerate=60` | 95.5 / 112.5 | 1967.0 / 0.0 | 1850.6 / 0.0 | 0.3% / 11.5% / 21.4% |

The 1.5.2 client defaults to 24 fps and 960×540 for its motion encoding mode.
It uses a separate `framerate_image_mode` setting when connected in JPEG/WebP
mode. The installed JavaScript also queues keyboard sends until an animation
frame and schedules asynchronous rectangle display on animation frames. KasmVNC's
server waits half a frame interval at the start of a fresh burst before
sending a framebuffer update. These are concrete scheduling steps in the
path, with no timing attribution to individual steps from this spike.
[KasmVNC server source](https://github.com/kasmtech/KasmVNC/blob/b3ab9ac88e5283e986c65236cf0ed7462ec1505e/common/rfb/VNCServerST.cxx).

In the composited test, toggling the counter proxy's TCP_NODELAY gave
73.5 / 98.7 ms with Nagle enabled. Setting both
`framerate_image_mode=60` and `framerate=60` in the URL gave
81.7 / 102.6 ms. Asking for `enable_threading=false` gave 97.1 /
107.5 ms. Each probe used 24 keypresses with no misses.

A server-side change did help. Set this in `kasmvnc.yaml`:

```yaml
runtime_configuration:
  allow_client_to_override_kasm_server_settings: false
encoding:
  max_frame_rate: 60
```

The Xvnc command line confirmed `-IgnoreClientSettingsKasm 1 -FrameRate 60`.
The browser still displayed 24 fps in its image-mode setting after connection,
so the URL override did not survive the client preset. In a 16-key diagnostic
run, dispatch to first upstream proxy data was about 1–2 ms in each backend.
First upstream to first downstream data dropped from **53.6 ms** at KasmVNC
defaults to **35.9 ms** with the server configuration. The equivalent noVNC
observation was **44.3 ms**. The diagnostic uses the first bytes around each
keypress, so it locates the delay to the round-trip path without attributing
all of it to framebuffer scheduling. The tuned 60-key run improved p95 by
24.0 ms, and the tighter server presets also reduced motion traffic. With
the same UDP block and `max_frame_rate: 60`, re-enabling client overrides
gave 90.3 ms p95 over 24 keys and 47.2 ms to first downstream proxy data.
That control supports server ownership of the rate and presets as the fix
for the slower tail in this setup. Client overrides affect multiple encoding
settings, so these probes do not isolate frame rate from the other presets.

KasmVNC also starts a UDP listener on the WebSocket port by default.
`ss -ltnp` showed TCP 8444 on loopback and `ss -lunp` showed UDP 8444 on
all IPv4 interfaces, even when the web client used WebSockets. The server
starts the UDP thread unconditionally and binds its socket to `INADDR_ANY`.
The tuned benchmark ran in a disposable Ubuntu service with
`SocketBindDeny=ipv4:udp`. Xvnc stayed interactive, its TCP socket stayed on
loopback, and no desktop UDP socket appeared. A separate rootless arm64
probe used `setpriv` with seccomp to deny IPv4 datagram socket creation in
Xvnc only. Explicitly setting `network.udp.public_ip` to loopback avoided
the server's STUN lookup. Xfce ran outside the filter. The browser completed
all 12 trial keys over SSH (62.5 ms median, 75.6 ms p95), and the listener
check showed only loopback TCP. For implementation, prefer an upstream
TCP-only switch if available, or package and verify a process-scoped filter
for each supported guest architecture. [Listener startup](https://github.com/kasmtech/KasmVNC/blob/b3ab9ac88e5283e986c65236cf0ed7462ec1505e/common/network/TcpSocket.cxx),
[UDP bind](https://github.com/kasmtech/KasmVNC/blob/b3ab9ac88e5283e986c65236cf0ed7462ec1505e/common/network/webudp/WuNetwork.cpp).

## Authentication and data channels

The display TCP listeners were observed on guest loopback. There was no
published display port. Authentication was disabled in the scratch runs,
so any process with access to those loopback listeners could use the
desktop. SSH authenticated the tunnel. The shipped browser endpoint also
needs session authorization and explicit WebSocket origin checks, since
local loopback is reachable by other processes and browser origins.

- **TigerVNC/noVNC.** `ss -ltnp` showed loopback-only RFB on 5901 (IPv4
  and IPv6) and websockify on loopback TCP 6080. TigerVNC supports
  `VncAuth`, password and TLS security types. websockify has HTTP auth
  plugins with `--web-auth`; noVNC can prompt for RFB credentials.
  Use server-side `SendCutText=0`, `AcceptCutText=0` and disable
  primary-selection exchange and runtime overrides of those settings.
  noVNC exposes received clipboard text and can send text via RFB.
  The legacy VNC path is a candidate for an opt-in SSH forward for macOS
  Screen Sharing. The native client was untested.
  [TigerVNC options](https://tigervnc.org/doc/Xvnc.html) and
  [noVNC API](https://github.com/novnc/noVNC/blob/v1.6.0/docs/API.md).
- **KasmVNC.** HTTP Basic auth uses accounts managed by `kasmvncpasswd`.
  It can separately block both clipboard directions through
  `data_loss_prevention.clipboard`, which maps to `SendCutText` and
  `AcceptCutText`, and can restrict content types and size. Keep both Kasm-specific and standard VNC client overrides disabled for
  security policy and leave UDP unavailable.
  KasmVNC's RFB protocol extensions preclude ordinary VNC viewers.
  [KasmVNC configuration](https://docs.kasmvnc.com/docs/configuration/).
- **Selkies.** `ss -ltnp` showed loopback-only TCP 8082 and the selected
  WebSocket mode opened no UDP display listener. Basic auth can use a
  separate view-only password. `--enable-clipboard=false` turns the
  clipboard off at the server. Directional modes exist, and file
  transfer has its own switch (`--file-transfers none`). Disable dual
  transport, command input, printing and device forwarding for this use.
  Keep the default same-origin restriction on the streaming WebSocket.
  [Selkies options](https://pypi.org/project/selkies/2.2.1/).

## Recommendation accepted by the owner on 2026-11-24

Start phase 2 with **KasmVNC at server-controlled 60 fps over WebSockets**,
provided the owner accepts a pinned GitHub release asset and the desktop
layer can enforce a TCP-only listener. It had the lowest scrolling bandwidth,
near-zero connected idle cost and p95 within 5 ms of the best run. It used
77% less bandwidth for scrolling and 56% less for video than TigerVNC/noVNC.
That combination fits an editing desktop that can sit open alongside agents.

**Selkies over WebSockets** had the fastest median and the lowest video
traffic. It is the better candidate if video or sustained screen motion
dominates. In the measured CPU-only mode it used 22% of the guest's
vCPU capacity and 11.3 KiB/s download while a connected desktop sat idle.
Its PyPI package is a separate source decision. **TigerVNC/noVNC** is the
Ubuntu-package fallback if third-party assets or KasmVNC's UDP mitigation
are unacceptable. It had the lowest p95 and higher motion traffic. macOS
Screen Sharing is a possible bonus for the fallback.

The owner accepted the pinned GitHub package and selected KasmVNC with
server-controlled 60 fps and UDP blocked. The spike used still captures for
visual inspection. Motion quality on the target laptop remains unmeasured.

## Proposed CLI and Desktop tab

- Make the guest desktop an opt-in Ubuntu profile layer. Put the Xfce
  session and component lifecycle behind one guest command with bounded
  start and readiness reporting, usable without systemd. Keep the
  display TCP target fixed to guest loopback. Verify listeners and
  keep existing host and guest network rules.
- `box desktop NAME` resolves the VM or an installed homebox alias and
  verifies its existing SSH host-key pin. Its forwarding session targets
  only the display endpoint. A client-side listener binds an OS-assigned
  loopback port and opens the browser after readiness. Serve the same web
  client for CLI and app. Authorize each browser session with a short-lived,
  single-use bootstrap capability exchanged for a scoped HTTP-only cookie.
  Validate the HTTP Host and WebSocket Origin and strip local authorization
  before proxying to the guest. Keep the tunnel in the foreground and close
  its listener and connections on interrupt or SSH loss. Closing the
  viewer leaves the Xfce session in the VM so that users can reconnect.
- Add **Desktop** to the existing VM detail view with an explicit
  **Connect** action, a connected viewer, and a reconnect action if the
  SSH channel drops. Desktop setup failures should identify the VM and
  surface the existing pin/SSH error or explain that its desktop layer is
  missing. Reuse the app's pinned SSH transport and the CLI's listener
  lifecycle and authorization. Load guest web content in an unprivileged
  view that cannot call Wails bindings, navigate to arbitrary hosts, or
  write to the host clipboard. Verify this as a WKWebView acceptance gate
  before enabling embed. Keep file transfer in the existing Files tab
  with its explicit native dialogs.
- Before enabling the feature, run `box doctor`, assert that each
  desktop TCP listener is loopback-only and that no UDP transport is
  exposed, and exercise pin mismatch, cross-origin WebSocket rejection,
  reconnect, clipboard policy and listener cleanup from a laptop. Repeat
  latency and traffic on an actual LAN and Tailscale path with the
  chosen server and an ordinary editing workload.
