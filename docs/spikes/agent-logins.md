# Agent login spike

2026-11-22. The lead inspected file metadata and JSON shape on a signed-in
Ubuntu guest. The CLI and shell probes used an unsigned Ubuntu 26.04
container with homebox's [mise](../../guest/common/mise.sh) and
[agent](../../guest/common/agents.sh) scripts. Two boots of the isolated
homebox scratch VM stalled before SSH became available. The Linux probes below
are container results.

## Claude Code

The Linux probe returned Claude Code `2.1.323`. Both `claude --help`
and `claude setup-token --help` advertised the long-lived subscription token
command. Only help and version commands ran.

`claude setup-token` opens browser authorization once and prints a one-year
OAuth token without saving it to disk. On Linux the CLI reads it from
`CLAUDE_CODE_OAUTH_TOKEN`. Model requests authenticate against the Claude
subscription on eligible plans. The token has model-request scope, so Remote
Control and claude.ai connectors need another login path. `--bare` skips this
variable. [Anthropic authentication](https://code.claude.com/docs/en/authentication#generate-a-long-lived-token)

The lead observed `~/.claude/.credentials.json` (mode `0600`, 554 bytes).
Its `claudeAiOauth` object had 108-character `accessToken` and `refreshToken`
strings, millisecond epoch `expiresAt` and `refreshTokenExpiresAt` fields
(about three hours and 27 days after that login, respectively), six `scopes`
entries, and string fields `subscriptionType`, `subscriptionTypeSource` and
`rateLimitTier`. The guest also had `~/.claude.json` (app state, mode `0600`,
about 160 KB) and `~/.claude/settings.json`. Anthropic documents this Linux
credential file and the `CLAUDE_CONFIG_DIR` override.
[Anthropic credential management](https://code.claude.com/docs/en/authentication#credential-management)

A Claude Code subscription-login report describes refresh rotation and stale
refresh tokens failing with `invalid_grant` in concurrent macOS sessions.
That is field evidence for the Claude account flow, with Linux cross-VM
behavior still untested. Once two VMs have separate copies, even coordination
within one guest cannot propagate the replacement to the other. A successful
request before expiry establishes only that the copied access token worked.
The three-hour observation is specific to the lead's login.
[Claude Code issue #80585](https://github.com/anthropics/claude-code/issues/80585)

## Codex CLI

The Linux probe returned Codex CLI `0.171.0`. `codex login --help` listed
`status`, `--device-auth`, `--with-api-key` and `--with-access-token`.

Plain `codex login` uses browser OAuth and a loopback callback. Without a
browser the user can open the printed URL on a laptop, but the callback must
reach the guest's listener. OpenAI documents forwarding local port 1455 over
SSH. Prefer `codex login --device-auth` for SSH-only guests. Enable device
login in account security settings or workspace policy, then complete the
one-time code in the laptop browser. Both flows use ChatGPT subscription
access and create a renewable session. `codex login --with-api-key` reads a
Platform key from stdin and uses usage-based API billing.
[OpenAI authentication](https://learn.chatgpt.com/docs/auth#login-on-headless-devices)

OpenAI also documents Enterprise Codex access tokens for approved
non-interactive workflows. Permitted members create them in ChatGPT and set
`CODEX_ACCESS_TOKEN` or pipe one to `codex login --with-access-token`.
Expiry is chosen within workspace policy, with documented examples of 7, 30,
60 and 90 days. An earlier Codex-only dialog can offer no expiration. The docs
present this as Enterprise automation, so availability for the owner's plan
is unverified. [OpenAI access tokens](https://learn.chatgpt.com/docs/enterprise/access-tokens)

The lead observed file-based `~/.codex/auth.json` (mode `0600`, 4.6 KB).
Fields included a seven-character string `auth_mode`, a null
`OPENAI_API_KEY`, string `last_refresh`, and a `tokens` object with
`id_token` and `access_token` (JWT-sized), `refresh_token` (196 characters),
`oauth_provenance` (`issuer`, `client_id`), `login_owner_id` and `account_id`.
Other files included `auth.json.lock`, `config.toml`, `installation_id` and
SQLite state. OpenAI documents the file under `CODEX_HOME` and alternative
credential storage modes. Both observed browser logins contain an access
token and a refresh token.
[OpenAI credential storage](https://learn.chatgpt.com/docs/auth#credential-storage)

OpenAI documents a cache copy as a headless fallback and automatic refresh
during use. The CLI source handles `refresh_token_reused` as a terminal
refresh failure, and a report on OpenAI's tracker describes reuse after
copying file-based credentials into separate stores. Treat a copy as a
point-in-time transfer. Independent refreshers can leave other copies on a
spent refresh token, and a guest-local lock cannot protect another guest.
[OpenAI headless login](https://learn.chatgpt.com/docs/auth#login-on-headless-devices),
[Codex refresh implementation](https://github.com/openai/codex/blob/rust-v0.171.0/codex-rs/login/src/auth/manager.rs#L1754-L1763),
[Codex issue #15502](https://github.com/openai/codex/issues/15502)

## SSH, remote editors and tmux

The probe used OpenSSH and a mode-`0600` guest file holding
`export HB_DUMMY=1`. Sourcing it from `~/.profile` alone reached
`bash --login -c` with a clean environment, while a plain `ssh vm cmd`
reported it unset. Adding the same source hook above Ubuntu's
non-interactive early return in `~/.bashrc` reached interactive SSH
without a remote command, plain SSH commands, and both `bash --login -c`
and `bash --login -ic` after clearing the inherited variable.

The login-shell probes model editor startup. A Microsoft maintainer
reported a non-interactive login environment for the VS Code Remote-SSH
ExecServer. A Cursor Remote-SSH diagnostic shows the bootstrap using
`bash --login -c bash`. These probes exercised equivalent shells. The
actual editor servers and extension hosts were outside this test.
[VS Code Remote-SSH issue #9091](https://github.com/microsoft/vscode-remote-release/issues/9091),
[Cursor Remote-SSH startup diagnostic](https://forum.cursor.com/t/ssh-remote-development-broken-on-update/136520)

A fresh tmux server inherited the dummy variable. A server started before
the hooks still had it unset in its global environment. A new login Bash
running under that old server read the file and saw it. A task launched
directly by an existing tmux server can inherit that server's cached
environment. Existing panes and remote editor processes also keep their
old environments until relaunched.
[tmux environment](https://man.openbsd.org/tmux#GLOBAL_AND_SESSION_ENVIRONMENT)

Put the proposed token in a guest-owned export file outside the startup
scripts, with mode `0600` in a directory with mode `0700`. Source it
silently from the effective login profile (stock Ubuntu uses
`~/.profile`) and before the early return in `~/.bashrc`. Keep the loader
idempotent because Ubuntu's `~/.profile` can source `~/.bashrc` too.
Restart pre-existing remote editor servers and renew stale tmux sessions
and panes on token changes. Put no token in SSH options, shell history or
system-wide environment.

## GitHub CLI

On Linux `gh auth login` prefers the system credential store and falls back
to a plaintext token in `hosts.yml` in its config directory, normally
`~/.config/gh`. `gh auth status` reports the storage location. `GH_TOKEN`
works for GitHub.com and takes precedence over stored credentials. The manual
recommends it for headless use and fine-grained PATs. Repository scoping limits
which repositories a leaked token can reach. All copies retain the same
permissions and share expiry and revocation.
[GitHub CLI login](https://cli.github.com/manual/gh_auth_login),
[GitHub CLI environment](https://cli.github.com/manual/gh_help_environment)

## Two-VM experiment pending owner approval

1. Create two disposable Ubuntu guests, `auth-source` and `auth-copy`, with
   fresh state and the same client versions. Use a test subscription account
   if available. Keep existing working VMs and host login files out of the
   test. Run one provider at a time. Close other processes for that provider
   in the test guests and avoid alternate credentials. Agree to the small
   subscription usage for the test requests.
2. Sign in on `auth-source` with the normal browser or device flow. For
   Codex, select file storage for both test guests. Run a minimal model
   request from an empty directory (request only the word “ok”, with no
   tools). Confirm the selected login type. Record only UTC times, exit codes
   and pass/fail. For Claude, read `expiresAt` locally as the scheduled
   checkpoint. For Codex, use the access token's expiry if available from its
   JWT claims, and record `last_refresh`. Keep all token values, claims other
   than expiry and account identifiers out of the record.
3. With the client closed, transfer just the credential file once using
   pinned SSH from the laptop. Stream directly from source SSH stdout into
   destination SSH stdin, with terminal allocation and tracing disabled.
   Write to a fresh guest-owned file with `umask 077`, then rename into place
   with mode `0600`. Keep no copy on the laptop or host. Try the same minimal
   request on the copy before expiry. Check locally whether either guest
   refreshed during this baseline and record only whether it did. If the
   copy has already refreshed, the intended source-first trial needs a fresh
   test login or should be classified as inconclusive.
4. Keep the copy idle. For Claude check near the recorded expiry (roughly
   three hours for the observed login), and just after it if needed. Start a
   fresh CLI on the source and make the same minimal request. Confirm an
   advanced `expiresAt` or `last_refresh` and record whether the refresh
   token changed using a local equality check that emits only a boolean.
   For Codex 0.171.0, start checking five minutes before JWT expiry, then
   again at expiry if needed. A successful source request
   without a confirmed refresh is inconclusive. Leave both guest clocks and
   credential file contents alone.
5. After a confirmed source refresh and the original access-token expiry,
   run a fresh CLI and the same request on the copy. An authentication
   failure, sign-in prompt, `invalid_grant`, or Codex
   `refresh_token_reused` is evidence of a failed copied session.
   `claude auth status` and `codex login status` can help identify local
   state. The request tests whether the provider actually accepts it.
   Network errors and quota errors are inconclusive. Recheck the source
   immediately to detect invalidation there too. A successful independent
   refresh on both sides would establish one successful cycle for that
   version and account.
6. Stop at the first authentication failure after checking the source once.
   Close test clients and discard both guests without saving images. Use
   provider session or token controls if revocation is needed. If the source
   or any working session loses access, sign in afresh after removing the copy.
   Restoring the old file cannot undo provider-side rotation or revocation.
