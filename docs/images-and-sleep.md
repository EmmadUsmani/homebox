# Images, sleep and resume: how they work underneath

Saved images are available on the macOS host with Lima's VZ driver. Sleep and
resume are future work in [plan.md](plan.md).

## What a running VM is made of

1. **Disk:** a file on the host. With Lima's `vz` driver, a standalone sparse raw disk file (`disk`, or
   `diffdisk` in older instances). It contains files, packages and logins.
2. **RAM:** the guest's memory, held by the VM's host process. Every running
   process, the kernel, the file cache, open sockets.
3. **CPU registers:** what each virtual CPU was doing at that instant.
4. **Device state:** network card queues, disk controller, timers, interrupt
   controller.

| Operation | Disk | RAM, CPU and devices |
|---|---|---|
| `box stop` (today) | Kept | Discarded; the guest shuts down cleanly first |
| Image | Copied | Not included |
| Sleep (saved state) | Left in place, untouched | Written to a file |
| Pause, or the host itself sleeping | Left in place | Kept in host memory; the CPUs stop being scheduled |

An image and a sleep file are near-complementary halves of a VM: one is the disk,
the other is everything else.

## Images

[Project image recipes](../images/README.md) reproduce project-maintained
VM disks locally from source.

- **Saving:** stop a VM and run `box image save NAME IMAGE`. Box
  archives the standalone disk and the VZ EFI variables used to boot it.
  APFS shares blocks through `clonefile`. Other filesystems fall back to a
  sparse copy. Computing the disk digest takes a
  full read of the logical disk even when APFS clones the data.
- **Using:** `box new NAME --image IMAGE` builds a fresh Lima instance with the
  usual user, index, ports and isolation settings. The driver stages the disk
  and EFI variables before the first start to skip Lima's disk preparation. It
  boots the saved disk without running the full profile scripts. Its disk is
  at least as large as the source allocation. The copied files keep their original ownership and
  paths, including any older accounts and home directories.
- **Identity:** Lima supplies a fresh hostname and cloud-init instance ID. A
  first-boot command replaces the disk's machine ID and SSH host keys. Box
  restarts the clone once so early services such as journald load the new
  machine ID, then pins the new SSH keys. Later starts keep that identity.
  Lima uses a per-boot cloud-init instance ID.
- **Export and import:** an archive contains a versioned JSON record and the
  raw disk, compressed as a single gzip file. The record includes the profile,
  sizes, time, box version, host architecture, EFI variables, and separate
  SHA-256 digests for the raw disk and EFI variables. Import verifies the
  disk length and both digests before publishing the image locally. Use
  `--name NAME` on import to choose a different local name.
  Use a Mac with the same CPU architecture. Only import images you trust.
- **Local store copies:** `box image copy IMAGE --from-home DIR` stages an
  independent disk in the active store and publishes it with the original
  metadata. On the same APFS volume it uses a copy-on-write clone. If cloning
  is unavailable, it warns and makes a sparse copy. It checks the disk size
  and trusts the recorded disk digest, so use a source store you control.
  The smoke runner uses this path for locally stored images.
- **Storage:** images contain disk data including logins, tokens, installed
  packages and settings. Save and export print a warning. Removing an image
  leaves its existing VMs and any exported files in place.

## Sleep and resume

Apple's Virtualization framework has this since macOS 14:

1. Pause the virtual CPUs.
2. `saveMachineStateTo` writes RAM, CPU and device state to a file, about the
   size of the memory the guest uses (up to its full allocation; whether Apple
   compresses it is unknown).
3. The VM process exits and its RAM returns to the host.
4. To resume: build a VM with *exactly* the same configuration on the *same*
   disk, call `restoreMachineStateFrom`, and continue. The guest kernel never
   rebooted; its clock just jumps forward (chrony or NTP corrects it).

QEMU has the same through its save and restore commands. Lima's `vz` driver
contains Apple's save-state call, but `limactl` (2.3) has no sleep command;
whether Lima exposes it is unverified.

**Why a sleep file isn't a temporary image:**

- **It's tied to its exact disk.** The saved RAM holds a file cache and file
  system state that assume the disk is byte-for-byte as it was. After a resume,
  the guest writes to the disk and the old state file no longer matches: it can
  be restored once.
- **It's tied to this host and configuration.** Same hypervisor, same VM
  config, and in practice the same machine and OS version.
- **No new identity.** A restored VM is the same machine mid-thought. Restoring
  copies would give several VMs the same SSH keys, machine ID, network lease and
  random-number state.

Disk plus RAM saved together is a full **snapshot** ("go back to exactly this
moment"), as in VMware or QEMU's `savevm`. Cloud sandboxes fork new VMs from a
booted snapshot with Firecracker, then patch identity and randomness, which is
the hard part. homebox doesn't plan snapshots.

## homebox-specific questions for the spikes

- **The gateway socket.** Each VM's second network card is a Unix socket to
  box's network daemon on the host ([architecture](architecture.md)). On resume
  it must reconnect; flows open at sleep time will drop. Today the card can't
  reconnect without a VM restart (see [to-do](todo.md)). Whether Apple's save
  state accepts a VM with this kind of network attachment at all is unknown.
- **Confinement.** The resumed VM process needs the same Seatbelt profile and
  gateway as before, set up by box before the restore.
- **Base images** must be built before anything is logged in, so no tokens are
  copied into every VM.
- **Disk space.** A clone costs nothing at first but grows as the VMs diverge; a
  sleep file costs up to the VM's memory size each.
