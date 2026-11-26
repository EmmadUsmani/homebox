# GUI and remote desktop smoke tests

Run on a fresh Ubuntu VM on the Mac. The host checks require Node.js and npm.

```sh
box desktop NAME --install
smoke/run.sh NAME gui
box doctor NAME
```

The guest setup installs GUI development libraries, Noto fonts, Mesa graphics
tools, PulseAudio and Electron, plus the browser dependencies. It adds
Playwright's Firefox and WebKit to the profile's existing Chromium install;
Firefox comes from Playwright's own distribution and runs without Snap. The
host phase installs the repository's pinned remote desktop harness dependencies
in a temporary directory and uses Playwright's Chromium on the Mac. Allow
several minutes on a fresh 2-CPU, 4-GiB VM for package and browser downloads and
for the checks to run.

- GTK4 (C), Qt 6 (C++), Tkinter, PySide6, SDL2, Swing and Electron each open
  an X window in the KasmVNC desktop. The test captures the window over X11
  and checks its dimensions and a known painted pixel.
- Graphics checks cover Mesa GLX renderer detection, EGL/OpenGL triangle
  rasterization and pixel readback on the desktop and without X, and a
  lavapipe Vulkan compute shader with exact buffer readback.
- Media checks cover Cairo PNG output, ImageMagick resizing, Noto font
  fallback and Pango rendering of Japanese, emoji and Arabic, VP8 encoding
  and decoding, and PulseAudio tone loopback through a null sink.
- Each Playwright engine responds to input and draws an expected pixel in a
  screenshot both headless and on the KasmVNC display. Chromium also reads back a WebGL
  pixel and plays the generated video.
- On the Mac, Chromium opens the one-time URL from `box desktop --no-open`,
  launches a fixture by typing into the Xfce terminal, checks visible pixels,
  and verifies mouse and keyboard input through a guest-side save marker.
  The same fixture exercises both directions of the guest clipboard helper
  used by the Desktop tab. An SSH connection separately requests X11
  forwarding and checks that the VM refuses it.

These checks exercise software graphics and loopback audio. GPU passthrough,
physical audio, network latency, and the native macOS clipboard controls are
outside this suite. The native viewer has separate verification scripts in
[desktop/scripts](../../desktop/scripts).

The guest's Ubuntu AppArmor policy blocks the bundled Chromium binary from
creating its process sandbox. Electron's bundled setuid helper isn't configured. The fixtures use local generated
content and run with Playwright's guest default and Electron's per-invocation
`--no-sandbox`. The Mac browser uses its process sandbox, and the suite uses
homebox's normal VM confinement. Electron's package manifest allows only its
pinned package to run the npm install hook that fetches its binary.
