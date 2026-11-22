# Guest profiles

Box opens a fresh SSH session for each step as the VM admin, using `sh -s` or
`sudo env … sh -s` for root steps. It streams the script on stdin and supplies
`HOMEBOX_PROXY_URL`, `HOMEBOX_NO_PROXY`, `HOMEBOX_DNS`, and `HOMEBOX_USER`.
The Ubuntu profile accepts an HTTP proxy URL with a numeric IPv4 address and
port, a comma-separated proxy bypass list, and a DNS endpoint in IPv4:port
form. Empty proxy and DNS values remove the corresponding managed settings.
An empty bypass list defaults to localhost, 127.0.0.1, and ::1.
If the admin name is empty, Ubuntu's root steps use `SUDO_USER`.
Scripts are POSIX sh, resolve no paths relative to their source, and return
nonzero on failure. Use a fresh SSH connection per step (OpenSSH `-S none`).
Multiplexed sessions reuse the proxy environment and group membership from
when their master connection opened.

A profile's `profile.yaml` lists boot images and ordered steps. Image architecture
uses `aarch64` or `x86_64`. Script paths are relative to this directory, so profiles
can share the scripts in `common/`. Package and service setup belongs to the
distro profile.

Ubuntu configures explicit proxy settings for SSH, apt, and Docker image pulls
before enabling transparent IPv4 TCP for the VM and Docker bridges. Containers
and builds use transparent TCP and the guest resolver. The resolver routes
queries over UDP or TCP to `HOMEBOX_DNS` and ignores DNS servers and search
domains advertised on Lima's primary interface.

Box runs `common/doctor.sh` separately over SSH as the admin. It can read the
script from the same catalog as the provisioning steps. The script uses curl,
getent, and passwordless sudo with `sg homebox-direct` for the direct probes.
Box supplies `HOMEBOX_PROXY_URL`, `HOMEBOX_DNS`, and space-separated
`HOMEBOX_DENY_TARGETS` entries of the form `host:port`. It can override the
default public URL (`https://example.com`) via `HOMEBOX_PUBLIC_URL`. The script
prints one result per check and returns nonzero if any check fails.

Ubuntu installs Node 24 LTS and Python 3.14 via mise. The agent installers run
without signing in. To select full permissions after signing in, use
`claude --dangerously-skip-permissions` or `codex --dangerously-bypass-approvals-and-sandbox`.
Tailscale is installed with the service stopped and disabled.
