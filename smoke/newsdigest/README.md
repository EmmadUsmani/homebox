# News Digest

Quick builds the app, runs Compose, migrates and seeds the database,
renders the fixture in guest Chromium and probes the published HTTP
port from the Mac. Full adds typecheck, lint, unit and integration tests and the Mac
Chromium render. The guest build and Chromium installation serve both
levels. Both levels require an explicitly configured checkout.

Exercise a real Node 22 and pnpm monorepo on the Ubuntu guest. The smoke
test builds the app, runs typecheck, lint and tests, and starts its
Postgres and Redis Compose services. It migrates the database and adds two
synthetic editions. Chromium checks the latest edition, archive and
historical detail on desktop light and mobile dark in both the VM and
macOS. The Mac also probes the published HTTP port with curl.

Run on a dedicated scratch VM with the default forward for guest port
8000 and an available guest port 9464. Compose assigns loopback database
ports so it can share a VM with the packaged databases in the services area:

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
