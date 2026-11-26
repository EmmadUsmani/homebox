# News Digest

Exercise a real Node 22 and pnpm monorepo on the Ubuntu guest. The smoke
test builds the app, runs typecheck, lint and tests, and starts its
Postgres and Redis Compose services. It migrates the database and adds two
synthetic editions. Chromium checks the latest edition, archive and
historical detail on desktop light and mobile dark in both the VM and
macOS. The Mac also probes the published HTTP port with curl.

Run on a dedicated scratch VM with the default forward for guest port
8000 and no services occupying guest ports 5432, 6379 or 9464:

```sh
SMOKE_NEWSDIGEST_REPO=/path/to/news-digest smoke/run.sh NAME newsdigest
```

With no checkout configured, the runner records a skip. Preparation sends
a Git archive of the checkout's HEAD over pinned SSH. Uncommitted files
and local configuration stay on the host. Setup installs Node 22 with
mise, uses the app's pinned pnpm version, and lets Playwright install its
matching Chromium build. The Mac needs Node and npm for its Playwright
check. Both machines need network access for downloads on the first run.

Allow 15–30 minutes for a cold run. Logs and screenshots from both browsers
are in the smoke results directory. The host phase stops the guest API
and removes the smoke test's Compose volumes. The API serves the built
web app without a worker, agent, API token or live data. Browser traffic
is restricted to same-origin GET requests.
