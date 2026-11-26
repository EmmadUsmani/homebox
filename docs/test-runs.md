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
| 2026-11-26T23:45:41.330Z | c518f9c8735a | no | Mac14,10 | 27.2.1 | change | default | static | PASS | 4/0/0 | 3.1s |
