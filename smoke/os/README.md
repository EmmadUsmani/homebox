# Operating system smoke tests

Run this area on a disposable Ubuntu homebox VM with two CPUs and 4 GiB.

```sh
smoke/run.sh smoke-services os
```

Setup adds cron and inotify-tools. The guest checks exercise system and
user systemd services, journal reads, automatic restart after failure, a
user timer, and a real cron job. They also watch 10,000 files, open 2,048
file descriptors, perform a 128 MiB direct disk round trip, confine an OOM
kill to a 64 MiB service cgroup, activate a temporary swap file, and verify
UTC, UTF-8 filenames, sudo, local accounts and clock synchronization.

The host check reconnects over SSH to verify tmux persistence, then stops
and starts the VM with `box` and checks a fixture file and enabled service
after boot. Allow up to a minute for cron and additional time for the VM
restart. The area removes its temporary swap, units, accounts and fixtures.

Ubuntu 26.04's uutils `dd iflag=direct` returned an invalid-input error on
the guest's ext4 disk. The check uses aligned `O_DIRECT` reads and records
both transfer rates in the guest log.
