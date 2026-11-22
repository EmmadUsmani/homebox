# Agent logins: sign in once per host

A design for keeping Claude Code and Codex signed in across new VMs. **Nothing
here is built or tested yet.** Statements about how the two tools store and
refresh their logins come from memory, not from checking; the spike below
replaces them with findings.

## The problem

Each VM starts signed out. Provisioning installs `claude` and `codex`
([guest/common/agents.sh](../guest/common/agents.sh)), but their logins are
files in the guest user's home, and a new VM has an empty home. So every
`box new` means signing in to both agents again in a browser, which gets old
once VMs are cheap to make and throw away.

## Goals

- Sign in once per host; every later `box new` starts with working agents.
- Keep the current model: no host mounts, nothing from the host's own home
  directory, the host's sandboxes unchanged, and the VM still the untrusted
  side.
- Opt-in, easy to see what's stored, easy to revoke or wipe.
- A step towards phase 3's credential proxy ([plan.md](plan.md), "Credentials"),
  not a detour: both need box to hold the credentials on the host.

## Not goals

- Hiding tokens from the VM. Today the VM holds them after a manual login; this
  design doesn't change that. Phase 3 does.
- Syncing logins between hosts. Each host signs in on its own, like images
  staying on their host.

## Approach: a host-side store that `box new` copies in

```
box auth add claude [--from VM]   # store a Claude login on the host
box auth add codex  [--from VM]   # store a Codex login on the host
box auth ls                       # which agents are stored, and when
box auth rm claude|codex|--all    # delete from the host (doesn't revoke)
box auth push NAME                # copy stored logins into an existing VM
box new NAME [--no-auth]          # copies stored logins in by default
```

1. **Getting a login onto the host.** Two ways, the spike picks which each agent
   needs:
   - **A long-lived token** made for machines without a browser. For Claude,
     `claude setup-token` is meant to do this, still billed to the
     subscription (unverified). Box asks for it with a hidden prompt and never
     echoes it. No token in a flag, an environment variable or the shell
     history.
   - **Harvest from a VM** that's already signed in (`--from VM`): box reads
     the agent's login file from that VM over SSH. The host then doesn't need
     the agents installed. The file comes from the untrusted side, so box
     treats it as data: a size limit, a format check, and it's never run or
     parsed beyond that.
2. **Storing it.** Under box's own state, for example `~/.homebox/auth/`,
   mode 0600, one file per agent. The hostagent and network-daemon sandbox
   profiles already deny reads of the home directory outside their own paths,
   so neither can read the store; a doctor check should prove that, like the
   existing `hostagent-home-read` check. The macOS Keychain is the
   alternative (encrypted at rest). It works for `box new`, which you run
   yourself, but may not work from a boot-time job with nobody logged in.
3. **Copying it in.** A provisioning step after "Coding agents" writes the
   login as the guest user over SSH (the same path provisioning already uses):
   a token goes in an environment file the login shell reads, and a login file
   goes where the agent expects it, both mode 0600. Box can't check that the
   login works without spending a request, so `box doctor` only reports whether
   the step ran.

## The open risk: shared refresh tokens

A normal browser login stores a short-lived access token plus a refresh token.
If the refresh token rotates on every use (common, unverified for these two),
two VMs sharing one copied login fight. The first to refresh gets a new pair,
and the old refresh token in the other VM stops working, which signs it out.
That's why a long-lived token is the first choice, and harvesting a login file
is only safe if the spike shows the refresh token doesn't rotate, or that each
VM's copy can refresh independently.

## Security trade-offs

- **Blast radius.** One stored login copied into every VM means a compromised
  VM leaks the login every VM uses. Revoking it signs out all of them. Separate
  tokens per VM would limit this, but minting one usually needs a browser
  sign-in, which is the thing we're avoiding. Accept a shared token for now,
  and say so in [SECURITY.md](../SECURITY.md) when this is built.
- **The host now holds secrets.** Today it holds none for the agents. The store
  stays outside every sandbox's readable paths, outside images and outside the
  repo, and `box auth rm --all` wipes it.
- **Images.** Base images are built before anything is signed in
  ([images-and-sleep.md](images-and-sleep.md)), so they never carry a stored
  login; a VM from a base image gets one from `box new`. A VM image saved from
  a signed-in VM still carries its login, as the design already says.
- **Phase 3.** The proxy keeps the token on the host and lends the VM only
  what each request needs. This store is where that token would live, so the
  proxy replaces step 3 (copying in), not steps 1 and 2.

## Spike: questions to answer first

1. Claude: does `claude setup-token` exist in the current version, what does
   it produce, how long does it last, is it billed to the subscription, and
   which environment variable or file does the CLI read it from on Linux?
2. Codex: is there an equivalent long-lived or device-code login, or only the
   browser login and API keys? (An API key bills API usage instead of the
   subscription, so it's a fallback, not the default.)
3. For both: where does the login live on Linux, and does copying it to a
   second machine work? Use two test VMs from one login, refresh on one, then
   check whether the other still works after its access token expires.
4. Does a token in an environment file reach every way an agent starts:
   interactive `ssh main`, `ssh main claude -p ...`, VS Code remote-SSH and
   tmux?
5. Should the GitHub CLI's token (`gh auth`) use the same store? Its
   fine-grained token is already per-repo scoped, so sharing it is lower risk.

Record the findings in `docs/spikes/` and update this doc with what was
decided.
