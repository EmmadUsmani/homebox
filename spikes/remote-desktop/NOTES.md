# Remote desktop spike: work in progress

Handover notes; the final write-up goes in docs/spikes/remote-desktop.md.

- `desktop.sh` installs a minimal Xfce plus TigerVNC, noVNC/websockify and
  KasmVNC 1.5.2 (pinned GitHub release asset, sha256-checked). Selkies 2.2.1
  installs from PyPI into a venv (`python3 -m venv /tmp/selenv &&
  /tmp/selenv/bin/pip install selkies==2.2.1`; arm64 cp314 wheels exist).
- Each server ran on the guest's loopback only, reached through
  `ssh -F <box ssh config> -N -L <port>:127.0.0.1:<port> <vm>`:
  - TigerVNC `:1` + `websockify --web /usr/share/novnc 127.0.0.1:6080 127.0.0.1:5901`
  - KasmVNC `kasmvncserver :2 -disableBasicAuth`, `~/.vnc/kasmvnc.yaml` with
    `network.interface: 127.0.0.1`, `websocket_port: 8444`, `require_ssl: false`
  - Selkies on Xvfb `:3`: `selkies --addr 127.0.0.1 --port 8082 --use-cpu true
    --enable-basic-auth=false --manual-resolution true --manual-width 1600
    --manual-height 900` (without manual resolution it fails to resize Xvfb).
- `harness/measure.js` drives the web client with Playwright through a
  counting proxy: keystroke latency, then idle / scrolling text / 720p video
  bandwidth and guest CPU.
- First results (canvas method, `results-*-canvas.txt`): noVNC median 29 ms,
  KasmVNC median 96 ms (unchanged by -CompareFB 0, fixed quality or client
  60 fps; cause not found). KasmVNC's client defaults to 24 fps and 960x540
  video mode. Bandwidth noVNC ~3.9 MB/s scrolling, KasmVNC ~2 MB/s, Selkies
  ~0.8 MB/s (H.264, but ~20 KB/s and ~22% guest CPU even when idle).
- The canvas method can't read Selkies' worker-drawn canvas; the screencast
  method (content-aware, decodes frames with jpeg-js) is written but
  untested and probably too slow at 1600x1000 — decode a smaller crop or
  lower maxWidth.
