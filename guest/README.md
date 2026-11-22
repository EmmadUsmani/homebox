# Guest profiles

Box opens a fresh SSH session for each step as the VM admin, using `sh -s` or
`sudo env … sh -s` for root steps. It streams the script on stdin and supplies
`HOMEBOX_PROXY_URL`, `HOMEBOX_NO_PROXY` and `HOMEBOX_USER` (which may be empty).
Scripts are POSIX sh, resolve no paths relative to their source, and return
nonzero on failure. The fresh sessions pick up proxy and group changes.

A profile's `profile.yaml` lists boot images and ordered steps. Image architecture
uses `aarch64` or `x86_64`. Script paths are relative to this directory, so profiles
can share the scripts in `common/`. Package and service setup belongs to the
distro profile.

Ubuntu configures a guest-wide proxy for SSH sessions, apt and Docker when
requested. The Docker client's default HTTP, HTTPS and no-proxy entries are
managed by the profile. A blank proxy URL removes the managed settings.

Ubuntu installs Node 24 LTS and Python 3.14 via mise. The agent installers run
without signing in. To select full permissions after signing in, use
`claude --dangerously-skip-permissions` or `codex --dangerously-bypass-approvals-and-sandbox`.
Tailscale is installed with the service stopped and disabled.
