# Agent factory

Run a fresh Codex turn for each small project step in a dedicated Ubuntu VM.
The guest needs Node.js, Git and a signed-in Codex CLI. Homebox's Ubuntu
profile supplies the tools. Sign in inside the guest and give Git access
only to the intended origin. Start with a private repository or a bare
repository in the guest.

From a homebox checkout on the host, install the harness over box's pinned SSH.

```sh
box new factory --cpus 4 --memory 6
tar -C factory -cf - run.js ITERATION.md MISSION.example.md homebox-factory.service |
  box ssh factory -- 'umask 077 && mkdir -p ~/agent-factory && tar -xf - -C ~/agent-factory'
box ssh factory
```

In the guest, clone the project into `~/factory-project`. Commit
`MISSION.md` (start from [the template](MISSION.example.md)) and
`STEERING.md` on the branch you intend to run. Configure the clone's Git
identity and make sure the branch exists on `origin`. Keep this checkout
dedicated to the supervisor. Edit steering from another clone and push it.

Install an executable `~/agent-factory/health-check` that runs the project's
tests and checks the running app, exiting nonzero on failure. It runs from
the project directory with a one-minute timeout. Keep runtime files and
test artifacts ignored or outside the clone. The supervisor stages all
unignored project changes after the check succeeds. If the app should keep
running between checks, start it as a separate guest service.

## One-turn smoke test

Run `node --test factory/run.test.js` from the homebox checkout for offline
checks with a stub Codex CLI. The change tier runs these checks too.

Use a disposable checkout and origin. In the guest, run the same path the
service uses with a single attempt and a 15-minute deadline.

```sh
cd ~/agent-factory
RUN_HOURS=0.25 flock --nonblock --close run.lock node run.js ~/factory-project --once
cat heartbeat.json
git -C ~/factory-project log -3 --oneline
```

Expect `phase: stopped`, `health: passed`, and `result: published` when a
new commit was pushed. `no_changes` also exits successfully after a passing
health check and a successful push. Compare the recorded `published` SHA
with `git ls-remote origin` from the guest clone.

## Longer runs

In the guest, install and start the optional systemd user unit. To keep it
running after logout, enable lingering for the guest user with
`sudo loginctl enable-linger "$USER"`.

```sh
mkdir -p ~/.config/systemd/user
install -m 644 ~/agent-factory/homebox-factory.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user start homebox-factory.service
cat ~/agent-factory/heartbeat.json
```

The default budget is two hours from launch. Set `RUN_HOURS` in a user-unit
override or on the manual command, between zero (exclusive) and 24 hours
(inclusive). Each Codex call is capped at 30 minutes and uses medium
reasoning with the configured model. The supervisor fetches and fast-forwards
before each turn. Command failures retry after a minute, and recognized
provider usage-limit failures retry after 15 minutes. Git divergence needs
manual reconciliation. Failed turns leave their work in the checkout for
the next turn to inspect. The heartbeat records the current phase and
latest results. Private logs sit in the indicated `run-*` directory.
Review logs before sharing and delete old run directories as needed.

Use `touch ~/agent-factory/STOP` to cancel the active command and stop the
loop. The supervisor sends TERM to its command group, then KILL after a
two-second grace period. Remove STOP before restarting. A new run requires a
clean checkout. Review any interrupted edits before committing or discarding
them. The deadline also cancels the active command.

Codex has full guest permissions. The owner-file and Git checks halt before
automatic publication if the agent changes the checked-out branch, HEAD,
origin URLs, or tracked owner files. A process with the same guest permissions
can bypass those checks. Use a dedicated VM containing only the projects and
credentials this task needs. See [secrets in the VM](../SECURITY.md#secrets-in-the-vm).
