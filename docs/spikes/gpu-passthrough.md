# RTX 3080 Ti passthrough on Linux

2026-11-28 · Read-only feasibility spike for [phase 5](../plan.md#phases).
The hardware topology supports a candidate assignment. Lima 2.3.0 has a
debug launch hook, and homebox needs explicit VFIO support before testing
the assignment. No device was assigned or reset during this spike.

## Host observations

| Check | Result |
| --- | --- |
| Host | Ubuntu 26.04.1 LTS, Linux 7.0.0-44-generic, Lima 2.3.0, QEMU 10.2.1 |
| CPU and firmware | Core i7-13700K supports VT-d. The ACPI DMAR table and Linux DMAR units were visible. [Intel specifications](https://www.intel.com/content/www/us/en/products/sku/230500/intel-core-i713700k-processor-30m-cache-up-to-5-40-ghz/specifications.html) |
| IOMMU | Active for PCI grouping. Linux exposed 17 groups and both GPUs pointed to DMAR units. There were no explicit IOMMU or VFIO overrides on the kernel command line. Unprivileged `dmesg` was denied, so interrupt remapping remains unverified. |
| RTX group 13 | Exactly two functions. RTX 3080 Ti `0000:01:00.0` (`10de:2208`, driver `nvidia`) and HDMI audio `0000:01:00.1` (`10de:1aef`, driver `snd_hda_intel`). |
| Display | The RTX was the boot VGA device and owned the only connected DRM output, HDMI. Intel UHD 770 `0000:00:02.0` (`8086:a780`) used `i915` in its own group 0. |
| Reset | RTX graphics advertised function-level and bus reset (`flr bus`). The audio function exposed no `reset_method`. Repeatable reset still needs testing. |
| VFIO | `vfio`, `vfio_pci`, `vfio_iommu_type1` and `iommufd` were installed as modules and unloaded. The inspecting shell's soft memlock limit was 8 MiB. |
| Secure Boot | Enabled according to `mokutil --sb-state`. |

The inventory used `lspci -Dnnk`, PCI driver and IOMMU links in sysfs, DRM
connector status, the IOMMU-specific fields from `/proc/cmdline`, `modinfo`,
`lsmod` and `mokutil`. Group numbers are observations from this boot.

## Lima launch path

The tagged default template documents `minimumVersion` and `cpuType`
under `vmOpts.qemu`. The built-in driver already creates an x86-64 Q35
machine. Its `Exe` function parses `QEMU_SYSTEM_X86_64` as an executable
plus optional arguments and labels the arguments as debugging-only. A
stock Lima experiment can use that hook to inject `-device` options. For
homebox, the existing launcher sets the variable itself to
`homebox-qemu` and constructs a clean environment. The integration point
is that existing wrapper, with the two device arguments generated from
validated per-VM state. [Lima 2.3.0 template](https://github.com/lima-vm/lima/blob/v2.3.0/templates/default.yaml) ·
[Lima 2.3.0 QEMU driver](https://github.com/lima-vm/lima/blob/v2.3.0/pkg/driver/qemu/qemu.go#L1243) ·
[Homebox launcher](../../internal/confine/profile_linux.go)

The QEMU prototype would attach the two functions to the same guest
PCIe slot, reserving a free Q35 root port after checking Lima's generated
topology. For example, with a free root-bus slot at `0x1c`:

```text
-device pcie-root-port,id=boxgpu,bus=pcie.0,addr=0x1c,chassis=20
-device vfio-pci,host=0000:01:00.0,bus=boxgpu,addr=0x0,multifunction=on
-device vfio-pci,host=0000:01:00.1,bus=boxgpu,addr=0x0.1
```

Keep the emulated display for the VM console and use the physical RTX for
compute. Inject the arguments only on real VM launches and preserve
Lima's probe invocations. The exact root-port placement and BAR allocation
need a real boot. [QEMU 10.2.1 VFIO PCI device](https://github.com/qemu/qemu/blob/v10.2.1/hw/vfio/pci.c)

## What box would need

1. Add Linux-only, opt-in, typed per-VM device selection. Resolve the
   full IOMMU group and verify both expected identities and drivers
   before launch. Fail closed if any additional endpoint appears or
   the group changes between validation and launch. Treat the group as
   exclusive, including across parallel starts. Start with cold VM
   start and stop, and keep the host PCI assignment outside portable
   images. Treat suspend and migration as unsupported pending explicit
   device-state testing.
2. Prototype the QEMU legacy group backend first. Give the VM launcher
   narrowly scoped Unix access to `/dev/vfio/vfio` and the current
   `/dev/vfio/<group>` node, and allow device read/write and
   `LANDLOCK_ACCESS_FS_IOCTL_DEV` on those exact nodes. Include narrow
   read access to the PCI sysfs paths QEMU needs. The hostagent installs
   an inherited Landlock layer before QEMU installs its own, so both
   layers must permit any accesses QEMU performs itself. Review whether
   passing pre-opened group descriptors can keep the hostagent from
   opening nodes after confinement. Validate the node identities,
   since missing paths currently get skipped when installing Landlock.
   Keep device rights out of the gateway and ordinary VM profiles.
   [QEMU legacy backend](https://github.com/qemu/qemu/blob/v10.2.1/hw/vfio/container-legacy.c) ·
   [Homebox Landlock rules](../../internal/confine/landlock_linux.go)
3. VFIO needs ioctls for group setup, DMA map/unmap, region and interrupt
   setup, and device reset. The current seccomp policy doesn't block
   `ioctl` or memory locking. Keep the network filter and check the
   actual request set on the prototype before considering any narrower
   filter. For legacy type1 VFIO, DMA-pinned guest pages count against
   `RLIMIT_MEMLOCK`. Set a finite inherited limit for the GPU-enabled
   launch path covering configured guest RAM plus measured headroom.
   Inspect the effective QEMU limits when launched through box and keep
   QEMU unprivileged. QEMU's optional whole-process `mem-lock` setting
   is separate from VFIO's pinning.
   [Homebox seccomp rules](../../internal/confine/seccomp_linux.go) ·
   [Linux type1 implementation](https://github.com/torvalds/linux/blob/v7.0/drivers/vfio/vfio_iommu_type1.c) ·
   [QEMU memory-lock option](https://www.qemu.org/docs/master/system/qemu-manpage.html)
4. Extend launch validation and doctor to check the assigned devices and
   confinement. Verify the approved group is accessible only on the
   opt-in VM. Exercise the existing host file and network denial tests
   with the GPU attached, including egress canaries through both NICs.
   An IOMMU limits device DMA to mapped pages, and interrupt remapping
   limits where device interrupts can land. The guest still gains access
   to a physical device and more QEMU, VFIO, IOMMU and firmware code
   paths. Keep unsafe interrupts, no-IOMMU mode and ACS overrides
   disabled. Real group membership and reset scope matter, especially
   for a consumer card that may fail to reinitialize after guest exit.
   [Linux VFIO security model](https://docs.kernel.org/driver-api/vfio.html) ·
   [QEMU PCI reset handling](https://github.com/qemu/qemu/blob/v10.2.1/hw/vfio/pci.c#L3587)

The separate IOMMUFD backend would need its own review of `/dev/iommu`,
the exact `/dev/vfio/devices/vfioX` nodes, and its ioctl and pinning
requirements. [Linux IOMMUFD interface](https://docs.kernel.org/userspace-api/iommufd.html)

## Owner steps for a hardware session

Perform steps 1–3 at the machine. Continue with step 4 once the confined
box prototype is ready.

1. Save work, record the current firmware and boot settings, and make
   sure the normal boot entry remains available. Move the monitor
   cable to the motherboard display output. In UEFI, enable integrated
   graphics and choose IGD/iGPU as the primary display. Confirm Intel
   VT-d and CPU virtualization (VT-x) are enabled and keep Secure
   Boot enabled. Boot normally first. Verify the login and desktop
   work on the Intel output and check the connected DRM connector
   resolves to `0000:00:02.0` before touching the RTX drivers.
2. Check the boot's privileged kernel log for DMAR and interrupt
   remapping (for example, `sudo journalctl -k -b | grep -Ei
   'DMAR|IOMMU|IRQ remap'`). Check the identities and both group
   memberships again. The observed boot already groups devices
   without `intel_iommu=on`. If the firmware change leaves IOMMU off,
   confirm VT-d first and use `intel_iommu=on` in the test boot entry
   if needed. Leave `iommu=pt` out of the initial test. Stop if
   interrupt remapping cannot be confirmed or a third endpoint joins
   the RTX group. [Kernel parameter reference](https://docs.kernel.org/admin-guide/kernel-parameters.html)
3. Close compute jobs using the RTX. Check that `10de:2208` and
   `10de:1aef` each match only the intended function with `lspci -Dn`.
   For early binding on Ubuntu with initramfs-tools, add `vfio_pci`
   to `/etc/initramfs-tools/modules` and stage a separate test boot
   entry using `vfio-pci.ids=10de:2208,10de:1aef`. Rebuild the
   matching initramfs with `sudo update-initramfs -u -k "$(uname -r)"`
   and check that it includes VFIO. After reboot, confirm both BDFs
   report `Kernel driver in use: vfio-pci` and the Intel desktop
   still works. If another driver claims either BDF first, fix early
   boot ordering before retrying. If IDs are shared, use a reviewed
   per-BDF early boot override. For rollback, boot without the
   VFIO ID parameter, undo the initramfs change and rebuild it, then
   return to the original firmware display selection and cable if
   desired. Leave the Intel display as the recovery console during
   testing. [Linux VFIO binding and group requirements](https://docs.kernel.org/driver-api/vfio.html)
4. With an approved per-group device permission and bounded memlock
   limit in place, start a disposable Ubuntu VM through the
   confined box prototype. Confirm `lspci -nnk` sees both functions
   in the guest. Install a compatible NVIDIA guest driver and CUDA
   toolkit, including any module-signing steps required by guest
   Secure Boot. Run `nvidia-smi` to check the GPU and driver, then
   build and run NVIDIA's `deviceQuery` and `vectorAdd` for the toolkit
   version in the guest. Require the RTX to be enumerated, the samples
   to pass, and doctor to pass. Repeat after a guest reboot and at
   least two full stop/start cycles without a host reboot. Review host
   logs for VFIO, PCI and DMAR faults and check the Intel console
   throughout. A card that requires a host reboot between runs fails
   the lifecycle check. [NVIDIA CUDA installation guide](https://docs.nvidia.com/cuda/cuda-installation-guide-linux/) ·
   [NVIDIA CUDA samples](https://github.com/NVIDIA/cuda-samples)

## Recommendation

Keep Lima first. Prototype a narrow opt-in change to `homebox-qemu` and
validate it during an owner-attended hardware session. Keep GPU support
out of ordinary launches until CUDA, reset and isolation checks pass.
If the confined Lima path cannot support the card, evaluate Incus as
a second box driver. Incus has a first-class VM `gpu` device with
`gputype=physical` and a PCI selector. Confirm that both functions are
claimed together and requalify host process, file, network, SSH and
lifecycle isolation for that backend before offering it through box.
Incus still depends on the same IOMMU grouping, remapping and device
reset behavior. [Incus physical GPU device](https://linuxcontainers.org/incus/docs/main/reference/devices_gpu/#gputype-physical)
