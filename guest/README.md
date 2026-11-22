# Guest profiles

Box opens a fresh SSH session for each step as the VM admin, using `sh -s` or
`sudo env … sh -s` for root steps. It streams the script on stdin and supplies `HOMEBOX_USER`.
If the admin name is empty, Ubuntu's root steps use `SUDO_USER`.
Scripts are POSIX sh and return nonzero on failure. Box opens a fresh SSH
connection for each step.

A profile's `profile.yaml` lists boot images and ordered steps. Image architecture
uses `aarch64` or `x86_64`. Script paths are relative to this directory, so profiles
can share the scripts in `common/`. Package and service setup belongs to the
distro profile.

Ubuntu uses the socket-backed NIC for IPv4 egress and sends DNS directly
to public resolvers. Ordinary Docker bridges use the same route via guest
masquerading. Reprovisioning removes earlier proxy settings, the transparent
TCP redirect and the Docker DNS override. Lima's built-in NIC carries SSH
and published services under host-side confinement.

Box runs `common/doctor.sh` separately over SSH as the admin. The script
checks public HTTPS, DNS over UDP, NTP and IPv4 echo, and probes the deny
targets supplied as space-separated `host:port` entries in
`HOMEBOX_DENY_TARGETS`. It also checks for host mounts and SSH agent access.
It prints one result per check and returns nonzero if any check fails.

Ubuntu installs Node 24 LTS and Python 3.14 via mise. The agent installers run
without signing in. To select full permissions after signing in, use
`claude --dangerously-skip-permissions` or `codex --dangerously-bypass-approvals-and-sandbox`.
Tailscale is installed with the service stopped and disabled.
