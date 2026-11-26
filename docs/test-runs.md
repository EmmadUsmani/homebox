# Local test runs

The [test runner](../scripts/test.sh) appends one row per run. Totals count
runner steps as pass/fail/skip. `scripts/test.sh last` reports default and
partial passes separately.

| UTC time | Commit | Dirty | Mac model | macOS | Tier | Scope | Areas | Result | Pass/Fail/Skip | Duration |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-11-26T21:40:18.519Z | 0dd064dde43f | yes | Mac14,10 | 27.2.1 | batch | partial | os | FAIL | 8/1/0 | 364.6s |
| 2026-11-26T21:44:13.706Z | 0dd064dde43f | yes | Mac14,10 | 27.2.1 | full | partial | gui | FAIL | 8/1/0 | 378.4s |
| 2026-11-26T21:46:19.122Z | 001760c43cf3 | yes | Mac14,10 | 27.2.1 | batch | partial | os | PASS | 9/0/0 | 349.8s |
| 2026-11-26T21:46:20.244Z | 001760c43cf3 | yes | Mac14,10 | 27.2.1 | change | default | os, runtimes | FAIL | 9/1/0 | 392.6s |
| 2026-11-26T21:50:08.041Z | d80187b2e538 | yes | Mac14,10 | 27.2.1 | full | partial | gui | PASS | 9/0/0 | 350.9s |
| 2026-11-26T21:53:20.628Z | fec775e2e194 | yes | Mac14,10 | 27.2.1 | change | default | os, runtimes | FAIL | 9/1/0 | 349.6s |
| 2026-11-26T21:59:18.681Z | d44d294488cb | yes | Mac14,10 | 27.2.1 | change | default | os, runtimes | PASS | 10/0/0 | 325.9s |
| 2026-11-26T22:54:52.662Z | 81d775ab302c | yes | Mac14,10 | 27.2.1 | batch | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e | FAIL | 15/1/1 | 1248.6s |
| 2026-11-26T23:03:39.171Z | 89d8dd080947 | no | Mac14,10 | 27.2.1 | full | partial | containers | PASS | 9/0/0 | 523.6s |
| 2026-11-26T23:09:22.914Z | f9db8e3bbd8c | no | Mac14,10 | 27.2.1 | full | partial | gui | PASS | 9/0/0 | 343.6s |
| 2026-11-26T23:31:03.686Z | f9db8e3bbd8c | no | Mac14,10 | 27.2.1 | batch | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e | PASS | 17/0/0 | 1299.3s |
| 2026-11-26T23:39:38.273Z | f9db8e3bbd8c | no | Mac14,10 | 27.2.1 | change | default | containers, gui, native, newsdigest, os, runtimes, services | PASS | 14/0/1 | 514.5s |
