# Wails desktop spike

2026-11-22 · Phase 1b, step 2. The prototype opens one native window, fetches
local VM status through the shared Go use case, and renders the raw JSON.

## Version and build

The [Wails v3.0.0 release](https://github.com/wailsapp/wails/releases/tag/v3.0.0)
was published on 2026-10-26, after the v3 beta. GitHub marked both it and
[v3.1.0](https://github.com/wailsapp/wails/releases/tag/v3.1.0) as normal
releases. v3.1.0 (2026-11-01) was the newest stable v3 release in the GitHub
release listing. The v3.2.0 nightly tags were prereleases. The newest stable
v2 was [v2.17.1](https://github.com/wailsapp/wails/releases/tag/v2.17.1).
The [official changelog](https://wails.io/changelog/) and
[GA announcement](https://wails.io/blog/wails-v3-released/) agree with the
release metadata. The skeleton pins v3.1.0 and uses the default cgo backend.

The app built on Apple Silicon with macOS 15.5 (24F74), Go 1.27.4 and Apple
Command Line Tools (macOS SDK 15.4). Packaging uses Node.js built-ins and
was tested with Node 22.14.0 and npm 11.0.0. The production build uses
`npm ci` with the committed lockfile, then runs TypeScript and Vite before
embedding the generated assets into the Go binary. The Wails CLI is optional
for the production build. Vite and its React plugin require Node 20.19.x or
later within Node 20, or Node 22.12 and later.
The only tool installed for the spike was
`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`, into Go's user
binary directory, for the dev-mode experiment. Go fetched module dependencies
into its cache, and npm installed locked packages into `desktop/frontend/node_modules`. The app declares macOS 15.0 as the deployment target. Passing
the target to Go's external linker avoids linker warnings about newer object
files being linked with an 11.0 deployment target.

## Module layout

[`desktop/go.mod`](../../desktop/go.mod) uses the root module's import path
plus `/desktop`, and replaces the root module with `..`. Its import path
therefore sits under the parent of `internal/app`. The actual desktop build
resolved that import and called `Service.ListStatus` through it. No workspace
file or root build tags were needed.

Both binaries get their local service from
[`internal/wiring`](../../internal/wiring/service.go). The CLI configures its
own executable for daemon launches. The desktop adapter binds only the local
read-only list operation, and its React frontend renders the JSON as a text child of `<pre>`.

From the root, `CGO_ENABLED=0 go build ./cmd/box` passed.
`go list -deps ./cmd/box | grep -i wails` produced no lines. On macOS the Go
binary still links standard OS libraries and frameworks listed by `otool -L`,
including System, resolver, CoreFoundation and Security. It has no WebKit or
Wails dependency. The cgo-disabled Linux cross-build was reported as a
statically linked ELF. The root format check, `go vet ./...`, Linux vet, Windows
build and race tests passed. Root `./...` skips the nested module. CI compiles
the desktop app in one separate step on the existing macOS runner; signed-in
GUI checks run locally.

## Native verification

The transcript below records the original spike. The current
[`verify.swift`](../../desktop/scripts/verify.swift) checks the VM inventory
and can drive the local lifecycle and log views with the scratch VM.
Set `HOMEBOX_VERIFY_BUSY_LOGS=1` when running the verifier with
`scratch-j` in a disposable `~/.hb-j-*` state directory. It appends 2,400
marked entries to that VM's network log and times the Guest filter's
Accessibility lookup and press once those entries are visible.

From the root, run `node desktop/scripts/verify.js`. It builds the production
bundle, opens it with `open` and a fresh scratch state directory, waits for
the native window, finds the WKWebView accessibility tree, reads the status
and JSON, presses **Refresh status**, and reads the next result. It finds
the window ID for that process via Core Graphics, captures it with
`screencapture -l`, quits the app, and removes the scratch state. It needs
Accessibility and Screen Recording permissions in the invoking macOS session.
The default screenshot path is `desktop/dist/homebox-status.png`. Pass a
path as the first argument to save it elsewhere.

```text
AX read: Status loaded (1), JSON []
AX press: Refresh status
AX read: Status loaded (2), JSON []
```

WKWebView exposed the web content as `AXWebArea`, the text through descendant
`AXValue` attributes, and the HTML button through `AXPress`. The script
verified an empty local inventory against real `limactl`, without starting a
VM or reading the owner's state. The screenshot showed the second result.
The app build worked with Command Line Tools alone. On this machine the CLT
Swift 6.1 helper failed because its SDK and compiler reported different builds
and duplicate SwiftBridging module maps. The existing Xcode 16.2 toolchain
ran the helper successfully.

## Development mode

The watcher in [`desktop/dev.yml`](../../desktop/dev.yml) starts Vite and a
Go build without the `production` tag. Build the frontend once so Go has its
embedded asset directory, then start the watcher from the desktop module:

```sh
cd desktop
npm --prefix frontend ci
npm --prefix frontend run build
wails3 dev -config dev.yml
```

Vite binds only IPv4 loopback. An earlier experiment with a Node static server
bound to `localhost` selected IPv6, and Wails' macOS asset proxy dialed IPv4.
The React and TypeScript toolchain follows Wails' template. The optional
typed-event plugin needs generated event bindings, so the spike omits it
and calls its single method through the pinned runtime.

The native dev window reported `Status loaded (1)` and JSON `[]` via AX.
A regular browser loaded the React app and connected to Vite, but the Wails
runtime reported that desktop bindings are unavailable in the browser.
The page reached `Status failed`; the same binding succeeded inside the
Wails window. Use the browser for frontend work with fixtures and the native
window for end-to-end checks. This matches the
[Wails dev-mode documentation](https://wails.io/guides/build/customization/#using-a-browser-for-development).

## Remaining technical questions

- xterm.js and Go SSH: verify PTY sizing and resize, bounded streaming and
  cancellation, keyboard and clipboard handling, and pin validation for
  local and remote connections.
- SFTP: verify server support for rename and the save conflict check, including
  the race between checking metadata and replacing the file. Bound transfers
  across the web-view bridge and handle disconnects and cancellation.
- Monaco: check embedded worker loading on the Wails asset scheme, plus
  keyboard shortcuts and accessibility in WKWebView. Keep the native AX
  checks for actions and use integration assertions for editor state that
  the accessibility tree omits.
- Local lifecycle buttons need an explicit daemon executable strategy before
  they can reuse the CLI's isolation setup.

## Ubuntu desktop build

Ubuntu 26.04 uses Wails' GTK4 and WebKitGTK 6.0 backend. The cgo build also
needs X11 headers for Wails' X11 shortcut implementation. The app builds
with the matching `box` binary in the same directory. The Ubuntu build and
real-window verification commands are in [Ubuntu host setup](../host-setup-linux.md#install-desktop-build-dependencies).

The native GTK viewer uses a separate ephemeral WebKitGTK context and
session. The isolation fixture runs the same C implementation under X11,
checks a real user gesture, and reopens the origin to check that the first
view's storage disappears. The fixture and the platform-specific limits
are documented in [Linux guest viewer](../SECURITY.md#linux-guest-viewer).
