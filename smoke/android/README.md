# Android Emulator smoke

Run from the host checkout against a disposable x86-64 Ubuntu homebox VM on
Linux with nested KVM. The emulator requires working hardware acceleration.
The area skips if the guest user cannot read and write `/dev/kvm`.

```sh
box new android --nested-virt --cpus 4 --memory 8 --disk 50
box ssh android -- 'sudo usermod -aG kvm "$(id -un)"'
box doctor android
SMOKE_ANDROID=1 smoke/run.sh android android
box rm -f android
```

To let the tiered runner create and remove the nested VM, run:

```sh
SMOKE_ANDROID=1 scripts/test.sh full --area android
```

The runner defaults to 8 GiB. Set `SMOKE_ANDROID_MEMORY_GIB` to give the
scratch VM a different memory allocation in GiB.

You can add `--image homebox-smoke` to either VM creation path to
start from a compatible Linux image. The CPU setting is chosen for the
new VM even if the source VM used a different setting.

Setting `SMOKE_ANDROID=1` opts in and accepts the Android SDK licenses
noninteractively. The next SSH session picks up the guest group membership.
See [Linux host setup](../../docs/host-setup-linux.md#enable-nested-kvm-for-android)
for host prerequisites. Use `SMOKE_RESULTS` to choose where the host runner
stores logs and `android.png`.

Setup installs OpenJDK 21, emulator runtime libraries, Android SDK
Command-line Tools 20.0, Platform-Tools, Build-Tools 35.0.0, the Emulator
and the API 35 AOSP x86-64 image. Expect several minutes on a cold cache
and several GiB of downloads and disk usage. The SDK is cached inside
the guest. The test creates a temporary AVD and APK signing key, forces
an accelerated cold boot with SwiftShader, waits up to five minutes for
Android, and reports time to `sys.boot_completed`. It builds the fixture
with the SDK tools, installs and launches it, asserts on its visible text,
and captures the screen over ADB. The emulator and temporary build and
AVD files are removed at exit. The guest's local ADB server can be reused.

The check runs in both quick and full modes when enabled. It uses emulator
console port 5580 in the guest and tests software graphics, Android boot and
a local app. Play services, physical GPU acceleration and emulator internet
connectivity are outside its coverage.
