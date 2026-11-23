# Agent logins

The owner signs in to Claude Code and Codex inside each VM today.
[Provisioning](../guest/common/agents.sh) installs the clients, and each fresh VM
starts with its own home directory. The goal is to cut the browser work for new
VMs while keeping the existing SSH boundary, VM isolation and opt-in control.
The [spike](spikes/agent-logins.md) records vendor guidance, the observed Linux
credential layout and the shell tests.

## Proposal pending owner approval

- **Claude Code.** Opt in to provisioning the subscription OAuth token minted
  by `claude setup-token`. The owner authorizes it once in a browser and
  provides it to box through a hidden prompt. It lasts one year and supports
  model requests, including normal headless CLI runs. Remote Control,
  claude.ai connectors and `--bare` need their documented authentication paths.
  Keep browser logins local to each VM because copied credentials can conflict
  when they refresh. [Anthropic authentication](https://code.claude.com/docs/en/authentication#generate-a-long-lived-token)
- **Codex.** Run `codex login --device-auth` per VM, with device login enabled.
  The owner can complete it in the laptop browser. Use the forwarded browser
  callback if device flow is unavailable. Keep the renewable session local to
  the VM. Enterprise Codex access tokens are a separate option for permitted
  workspace automation. An API key uses usage-based API billing and requires a
  separate opt-in. [OpenAI authentication](https://learn.chatgpt.com/docs/auth#login-on-headless-devices)
- **GitHub CLI.** Keep explicit per-VM setup as the default. Consider an
  opt-in `GH_TOKEN` sourced from the same type of protected guest file if the
  owner wants shared provisioning. Use a fine-grained PAT restricted to the
  intended repos and permissions. [GitHub CLI authentication](https://cli.github.com/manual/gh_auth_login)

For the proposed Claude token, box would keep an owner-only file in its own
host state and send it over the pinned SSH provisioning path to a guest-owned
file with mode `0600` in a directory with mode `0700`. On the stock Ubuntu
profile, source a silent, safely serialized export file from the login
profile and early in `~/.bashrc`, before the non-interactive return, using the
same startup points as [mise](../guest/common/mise.sh). Fresh shells and their
children pick up changes. Restart existing remote editor servers and renew
tmux sessions and panes that retain older environments.

## Security and lifecycle

- The host becomes a credential holder. Verify that the hostagent and network
  process sandboxes cannot read the store. Keep secrets out of command-line
  arguments, provisioning output and image build inputs.
- A guest user and processes running as that user can read a provisioned
  token. An exported token also reaches child processes. Every copy of a
  shared token has the same permissions and revocation blast radius.
- Removing the host copy stops future provisioning. Existing guest files and
  running processes retain their copies. Revoke a compromised token with the
  provider and remove the guest copies.
- Base images are provisioned before sign-in. A manually saved image of a
  signed-in VM contains guest credential files under the existing
  [image design](images-and-sleep.md). Treat the image as sensitive.
- The [phase 3 credential proxy](plan.md#phases) is the design for keeping
  credentials out of the guest.

The [two-VM refresh experiment](spikes/agent-logins.md#two-vm-experiment-pending-owner-approval)
requires explicit approval before touching any real login. A duplicate
refresh attempt may invalidate the source session as well as the copy.
