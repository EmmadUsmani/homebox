# Desktop app: approved look

The owner approved these screens on 2026-11-22. They are the target for the
desktop app (phase 1b, step 2); what each screen does is in
[observability.md](../observability.md#the-desktop-app). The mockup sources are
in [desktop-mockups/](desktop-mockups): static HTML with inline styles, written
for a design canvas, so read them for layout, spacing and colours rather than
running them. Their data is made up.

Frontend: **React with TypeScript** (owner, 2026-11-22), with xterm.js for the
terminal and Monaco for the editor.

## Screens

| Mockup | Screen |
|---|---|
| [Main.html](desktop-mockups/Main.html) | VM overview: sidebar of VMs, header with state and actions, resource cards, servers, blocked network attempts |
| [Logs.html](desktop-mockups/Logs.html) | Live logs: source filter chips, unit filter, follow toggle, monospace stream |
| [Terminal.html](desktop-mockups/Terminal.html) | Terminal: session tabs, xterm.js area, footer noting pinned keys and no forwarding |
| [Files.html](desktop-mockups/Files.html) | Files and editor: tree with upload and download, Monaco editor, save blocked when the file changed in the VM |
| Desktop | Connect to Xfce, see connection or installation progress, install a missing layer, and reconnect after SSH loss |
| [Remote.html](desktop-mockups/Remote.html) | A VM on another Mac: the host-key-changed alert and the other reachability hints |

## Layout

- A window title strip (40 px), then a row: the **sidebar** (240–280 px) and
  the **main** area.
- The sidebar groups VMs under "This Mac" and "Other hosts". Each row has a
  status dot, the name (remote ones add "· host"), and the state in words.
  "Add a host" sits at the bottom.
- The main area starts with the VM's name, status pills and actions (Run
  doctor, Stop or Start, and the primary "Open terminal"), then the tabs
  Overview, Logs, Terminal, Files and Desktop.
- Content pads 28 px left and right; cards sit in grids with 16 px gaps.

The Desktop tab reuses the approved surface, button and warning tokens.
Its status panel shows **Connect**, VM-specific connection or installation
progress, **Install desktop** for a missing layer, or **Reconnect** after
SSH loss. Stopped VM and host-key errors use the app's existing status and
warning styles. The connected viewer fills the tab beneath the VM header.
The owner approved the Desktop tab's look on 2026-11-24.

The connected view adds **Paste to VM** and **Copy from VM** above the
viewer using the existing toolbar button tokens. The toolbar shows transfer
feedback. The layout and styling of these additions are pending owner review.

## Tokens

| Token | Value |
|---|---|
| Window background | `#0f1115` |
| Sidebar, title strip | `#12151b` |
| Card | `#151821`, border `#232733`, radius 12 px |
| Code and log areas | `#0b0d11` |
| Selected row | `#1d2330` |
| Text | `#e6e8ee`; secondary `#c3cad6`; muted `#a3abba` |
| Accent (links, active tab, bars) | `#5b9cff`; primary button `#3b82f6` |
| OK | `#3fb68b` (dot), text on dark `#7ad8b0` |
| Warning | `#f0a040` (dot), text `#f3b866` |
| Error | `#ef6b6b` (dot), text `#ff8a8a` |
| Buttons | 36 px high (32 px in toolbars), radius 8 px, border `#2c3240`, fill `#171b23` |
| Fonts | Inter 400/500/600 for the interface; JetBrains Mono for code, logs, ports and paths |

Bundle the fonts with the app; the mockups load them from Google Fonts only
because they're mockups.

## Rules carried from the design

- State is never colour alone: every dot has a word next to it.
- Text from a VM (log lines, process and file names, file contents) is shown
  as text, never as HTML. Escape codes are already removed by box's core.
- Buttons are real buttons, links are real links, and icon-only buttons have a
  label, so keyboard and screen readers work, and so the app can be driven
  through macOS Accessibility for testing.
- The editor refuses to save over a file that changed in the VM since it was
  opened, and says so.
