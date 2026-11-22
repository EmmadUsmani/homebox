# Images, sleep and resume: how they work underneath

Background for building images (phase 2) and sleep and resume (later) in
[plan.md](plan.md). **Nothing here has been tested with homebox yet**; it's
how the pieces work in general, plus what we know about Lima. A spike replaces
the guesses with findings.

## What a running VM is made of

1. **Disk:** a file on the host. With Lima's `vz` driver, a sparse raw file
   (`diffdisk`) on top of the downloaded distro image (`basedisk`). Files,
   packages, logins.
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

- **Saving:** stop the VM so the disk is consistent, then copy its disk file. On
  APFS a copy can be a clone: instant, sharing blocks until either file changes.
  `limactl clone` copies a stopped instance; whether box uses it or copies the
  disk itself is open.
- **Using:** a new VM gets its own clone and boots it from scratch.
- **Fresh identity on first boot:** the new VM regenerates its machine ID,
  hostname, SSH host keys and cloud-init instance ID, and box pins the new keys.
  Without this, every VM from one image would share an identity.
- **Reusable and portable:** one image can start any number of VMs, at any time.
  A disk file also works on another host, and under QEMU after a format
  conversion (raw to qcow2).
- **From a running VM:** pausing, cloning the disk and resuming gives a disk
  that boots like a machine that lost power (localbox appears to do this). It
  usually works but can lose writes still in memory. homebox starts with images
  from stopped VMs.

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
