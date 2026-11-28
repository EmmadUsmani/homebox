# macOS guests on Lima

The goal is a macOS guest with the same host and LAN isolation as an Ubuntu
guest, usable over pinned SSH and able to run the iOS Simulator. This spike
used an Apple M2 Mac with 32 GiB RAM, macOS 27.2.1 and Lima 2.3.0.

## Installer and first boot

The installed `_images/macos-27.yaml` template selects Apple's macOS 27.2
(`26B123`) arm64 restore image. The CDN reported 25,595,669,503 bytes
(23.84 GiB). The download took 9m 45s. Lima normally stores the download at
`~/Library/Caches/lima/download/by-url-sha256/<URL hash>/data`.
An instance uses `image` and an `image.ipsw` hard link during installation.
Lima removes the instance links after installation. Its cache persists.

Lima prepares the disk when creating the instance. On macOS 26 and later
it defaults to an ASIF disk for this guest. Its VZ installer configuration
attaches the primary disk and a virtual graphics device and has no
network devices. After installation, Lima mounts the guest disk's Data
volume on the host to install a launch daemon and writes root ownership
into the guest's APFS metadata. On boot that daemon runs
`lima-guestagent fake-cloud-init` from a separate read-only configuration
disk. The guest agent enables `sshd`, creates the admin account, and
installs its authorized keys. The default sudo policy is passwordless for
the shutdown command only. The generated GUI password is stored inside
the guest. See [the VZ disk setup](https://github.com/lima-vm/lima/blob/v2.3.0/pkg/driver/vz/vz_driver_darwin.go)
and [the macOS guest agent](https://github.com/lima-vm/lima/blob/v2.3.0/pkg/guestagent/fakecloudinit/fakecloudinit_darwin.go).

## Networking and SSH

On normal boot, the VZ driver always creates a user-mode NIC. An empty
`networks` list leaves it attached. `networkOpts.hostLoopback: false`
disables the special route to host loopback services through Lima's
user-mode gateway. It does not disable the NIC or block the rest of
its egress. `vzNAT` is a separate, optional Apple NAT attachment.
A `networks[].socket` entry adds a file-handle NIC using the same
QEMU-style Ethernet framing that homebox already uses. Stock Lima 2.3.0
has no YAML setting to make that socket the only NIC. See
[Lima's VZ network attachment code](https://github.com/lima-vm/lima/blob/v2.3.0/pkg/driver/vz/vm_darwin.go)
and [Lima's user-mode networking guide](https://lima-vm.io/docs/config/network/user/).

Lima's default SSH port forwarding for a macOS guest uses its primary
user-mode network. A socket-only guest therefore needs a different inbound
SSH path. The macOS guest also lacks Lima's automatic application port
forwarding. See [Lima's macOS guest guide](https://lima-vm.io/docs/usage/guests/macos/).

The test build kept the stock VZ installer and guest agent. In
`attachNetwork`, it bypassed Lima's normal NIC setup for macOS and
accepted either zero NICs or one `socket` NIC. The latter used Lima's own
`dialQemu` and `newVirtioFileNetworkDeviceConfiguration` path. The test
build kept the virtual graphics device but disabled the host viewer window.
The guest used the macOS 27 image template, no directory mounts, no agent
forwarding, four vCPUs, 8 GiB RAM and a 40 GiB disk. The installer image was
verified against the template SHA-256 and hard-linked from a scratch
download to the instance's `image` path to avoid a duplicate copy.

The installer completed in 3m 48s, and disk patching took about five seconds.
With zero virtual NICs attached, the guest launch daemon started 16 seconds
after VZ reported the VM running. A guest-side probe confirmed macOS 27.2
(`26B123`), no VirtioFS mount, an admin user with authorized keys, and a
local OpenSSH 10.5 response. The guest agent reported a DNS setup error
because it found no Ethernet service. Lima opened its usual host SSH
listener, but the forward had no route to the guest.

The 40 GiB ASIF disk occupied 25.8 GiB on the host after first boot.
The minimum sampled free space during installation was about 63 GiB.

On a second boot, the guest exposed an additional interface for the
socket NIC. The passive socket received no frames with the device left
unconfigured. On the next boot, an in-guest probe matched the interface
to the configured MAC, brought it up with a test-only IPv4 address,
and tried to reach a peer on the same test subnet. The Unix socket
received 14 framed ARP and IPv4 packets. The receiver discarded every frame
and provided no DHCP, gateway or host access. This confirms the guest
can send through Lima's file-handle NIC. Lima's default SSH forward still
targeted the disconnected user-mode network, so `limactl start` timed
out waiting for SSH even though VZ and the guest had started.

## From the spike to the confined profile

The unmodified homebox Seatbelt profile failed a macOS start preflight before
launching a VM. Homebrew's Darwin guest-agent binary was installed, and
Lima reported it missing under confinement. Unlike Lima's Linux plain mode,
macOS still needs that binary. A profile needs a narrowly scoped way for
the confined launcher to read the packaged agent as it builds the
configuration disk.

The [macOS profile](../../README.md#macos-guests-on-apple-silicon) uses stock
Lima and keeps its user-mode NIC for inbound SSH under Seatbelt. The guest
uses the socket NIC for egress. The initial profile targets macOS Sequoia
15.6.1 because the installed Xcode 16.2 supports macOS 15.x. See
[Apple's Xcode system requirements](https://developer.apple.com/xcode/system-requirements/).

- Lima's `hdiutil makehybrid` exited successfully but built an ISO full of
  empty files under the original filesystem policy. A control using a
  13-byte input reproduced the failure. Allowing metadata access to the
  exact APFS Data mount point restored the contents.
- Sequoia declined its DHCP offer because the gateway answered ARP for the
  offered address during macOS duplicate-address detection. Limiting the
  gateway's ARP handling to its own address fixed DHCP.
- A fresh Sequoia guest passed doctor after restarting with its original
  exported SSH pins. Doctor reported 60 passes, zero failures and one skip
  for Lima's local ICMP reply. A separate client identity connected through
  the SSH relay over loopback and verified noninteractive sudo and denial
  of SSH agent forwarding.

## iOS Simulator

An M2 host transferred the local Xcode 16.2 app bundle to the confined
Sequoia guest with tar over pinned SSH. In the guest, `xcodebuild -license
accept` and `xcodebuild -runFirstLaunch` completed noninteractively.
CoreSimulator refused to stage the 8.1 GiB runtime image on the 55 GiB
guest disk. A fresh guest with 4 vCPUs, 8 GiB RAM and an 80 GiB disk
imported the host's iOS 18.3.1 runtime (`22D8075`) using
`simctl runtime add --move`. Both assets were already installed locally,
so staging required no Apple ID sign-in.

The guest compiled a two-label SwiftUI app with the iOS Simulator SDK,
booted an iPhone 16 headlessly with `simctl`, and installed and launched
the app. `simctl bootstatus` finished in 45 seconds. The screenshot taken
with `simctl io booted screenshot` rendered both labels at 1179 × 2556.
The scratch VMs and both restore-image downloads were removed.

## Screen Sharing over pinned SSH

A fresh macOS 15.6.1 guest with four vCPUs, 8 GiB RAM and a 45 GiB disk
authenticated browser sessions through Apple's Screen Sharing service. A
prototype copied Apple's launchd job, kept its stock service disabled on
boot, and loaded the copy with `launchctl load -F`. The copy used the same
service label and explicit IPv4 and IPv6 loopback sockets on TCP 5900.
It omitted Bonjour. The guest reported only `127.0.0.1:5900` and
`[::1]:5900` as listeners. After restarting, the stock job was still
disabled and TCP 5900 remained closed until the copy was loaded.

The host reused the desktop proxy's pinned SSH connection, one-time
bootstrap link, cookie and Host/Origin checks. It served noVNC 1.7.0
from the executable and bridged binary WebSocket frames to RFB over SSH.
The prototype used the guest account through Apple's RFB authentication
type 30. It left the legacy shared VNC password option off. Headless
Chromium authenticated, received a 1920 × 1200 canvas, and reconnected
after reload. The framebuffer stayed black in both connections, including
after a separate fixture established a logged-in console session. The
guest's Screen Sharing agent reported `postEventFlag 0 screenCaptureFlag 0`.
Apple's extended session negotiation also authenticated without producing
a visible desktop. Guest doctor reported 60 passes, zero failures and
one skip for Lima's local ICMP reply.

The launchd setup proves the transport and listener confinement. A usable
desktop still requires macOS to grant the Screen Sharing agent capture and
input permissions. Apple's [Screen Sharing instructions](https://support.apple.com/guide/mac-help/mh11848/mac)
enable the service through System Settings, and its
[Remote Management documentation](https://support.apple.com/guide/remote-desktop/apd8b1c65bd/mac)
describes the command-line restrictions. The next spike should establish
a supported permission setup and repeat the pixel, input and reconnect
checks with a loopback-only listener.
