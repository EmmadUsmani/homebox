# Desktop and CLI parity

Host management runs on the machine hosting the VM. For VMs on other hosts,
the app uses the laptop’s pinned SSH connection to the guest. See
[observability](observability.md#the-desktop-app). The Linux build uses the
same controls. VM-backed Linux verification is tracked in [To do](todo.md#observability-and-the-desktop-app-phase-1b).

| CLI command or flag | App equivalent | Status |
|---|---|---|
| `desktop NAME` | Desktop tab | Done |
| `desktop --install` | Install desktop, then Connect | Done |
| `desktop --no-open` | Native viewer connects over a private SSH tunnel | CLI-only output for opening a browser manually |
| `doctor [NAME]` | Run doctor for the selected local VM | Done; run per VM for a host-wide check |
| `host init --authorized-key --lan --no-lan --ssh-include` | First-run host setup | Built by the lifecycle work |
| `new --nested-virt` | CLI | Linux QEMU/KVM creation flag and status value |
| `new --force-provision` | CLI | Desktop Create VM uses the default profile base behavior |
| `image rebuild / save / ls / rm / copy / export / import` and `new --image` | CLI | Pending desktop support |
| `logs NAME -f/--follow --source --unit` | Logs tab with follow, source and unit controls | Done |
| `logs -n/--lines` | Recent lines per source | Built in this change |
| `logs --json` | Structured log records displayed in the Logs tab | CLI-only machine output |
| `ls` | VM sidebar | Done |
| `ls --json` | VM sidebar uses the same inventory | CLI-only machine output |
| `new NAME --profile --cpus --memory --disk` | Create VM form | Built by the lifecycle work |
| `open NAME [GUEST_PORT]` | Open a published server in Overview or Settings | Done in Overview and Settings |
| `open --print` | Copy the published URL | Built in this change |
| `ports NAME --add --rm --set` | Published ports list and editing in Settings | Built in this change |
| `provision NAME` | Re-run provisioning | Built by the lifecycle work |
| `repin NAME --yes` | Review fingerprints and confirm in Settings | Built in this change |
| `resize NAME --cpus --memory --disk` | View or edit VM size in Settings | Built in this change |
| `rm NAME -f/--force` | Delete VM with confirmation | Built by the lifecycle work; bypass is for CLI automation |
| `ssh NAME [-- cmd...]` | Terminal tab for an SSH shell or guest commands | Done; direct execution from a host script is CLI-only |
| `ssh-config --host --include --installer --known-hosts` | Copy laptop SSH setup from Settings | Built in this change |
| `start NAME` / `stop NAME` | Start and Stop in the local VM header | Done |
| `status NAME` | Overview with uptime, doctor and resource readings | Done, including local process, container and provisioning details |
| `status --json` | Overview uses the same status data | CLI-only machine output |
| `top [NAME]` | Live resource readings in the selected VM’s Overview | Per-VM view done; no combined live resource table |
| `top --json` | Overview uses structured resource readings | CLI-only machine output |
| `version` | About homebox | Built in this change |
| `help`, `-h`, `--help` on all commands | Named controls and contextual guidance | CLI help describes shell syntax |
