# iOS Simulator smoke

Run on an Apple silicon Mac with Lima and a full Xcode selected by
`xcode-select` or `DEVELOPER_DIR`. Install a signed iOS Simulator runtime on
the host with the same major release as Xcode's Simulator SDK. The fixture
uses an iPhone 16 and targets iOS 18 or later. Xcode 16.2 with iOS 18.3.1 is
the combination used in the [macOS guest spike](../../docs/spikes/macos-guests.md#ios-simulator).

```sh
SMOKE_IOS=1 scripts/test.sh change --area ios
```

This area provisions a fresh macOS guest with four CPUs, 8 GiB of RAM and
an 80 GiB sparse disk. Run it on its own, without image flags. Leave space
on the host for Apple's restore image, the installed guest and the staged
toolchain. Allow several minutes for first boot and staging. With an existing
disposable macOS guest of the same size, use
`SMOKE_IOS=1 smoke/run.sh NAME ios`. Direct runs also need Node.js on the host.

Opting in accepts the Xcode license inside the guest and runs Xcode's
first-launch setup there. The host sends the Xcode app bundle and the
selected runtime image over pinned SSH. Host accounts, keychains and user
Library contents stay on the host. The test builds and signs its local SwiftUI
fixture, boots a headless simulator, installs and launches the app, and checks
the view's appearance log for its text. The runner saves
`ios-simulator.png` in its area results. The test removes its simulator,
imported runtime and Xcode copy. Xcode first-launch changes are scoped to
the disposable guest.
