# Local test runs

The [test runner](../scripts/test.sh) appends one row per run. Totals count
runner steps as pass/fail/skip. `scripts/test.sh last` reports default and
partial passes separately. The VM source is recorded for new runs. The
2026-11-27 01:30 UTC duration includes laptop sleep during VM boot.

| UTC time | Commit | Dirty | Host model | OS | Tier | Scope | Areas | Result | Pass/Fail/Skip | Duration | VM source |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-11-26T21:40:18.519Z | 0dd064dde43f | yes | Mac14,10 | 27.2.1 | batch | partial | os | FAIL | 8/1/0 | 364.6s | — |
| 2026-11-26T21:44:13.706Z | 0dd064dde43f | yes | Mac14,10 | 27.2.1 | full | partial | gui | FAIL | 8/1/0 | 378.4s | — |
| 2026-11-26T21:46:19.122Z | 001760c43cf3 | yes | Mac14,10 | 27.2.1 | batch | partial | os | PASS | 9/0/0 | 349.8s | — |
| 2026-11-26T21:46:20.244Z | 001760c43cf3 | yes | Mac14,10 | 27.2.1 | change | default | os, runtimes | FAIL | 9/1/0 | 392.6s | — |
| 2026-11-26T21:50:08.041Z | d80187b2e538 | yes | Mac14,10 | 27.2.1 | full | partial | gui | PASS | 9/0/0 | 350.9s | — |
| 2026-11-26T21:53:20.628Z | fec775e2e194 | yes | Mac14,10 | 27.2.1 | change | default | os, runtimes | FAIL | 9/1/0 | 349.6s | — |
| 2026-11-26T21:59:18.681Z | d44d294488cb | yes | Mac14,10 | 27.2.1 | change | default | os, runtimes | PASS | 10/0/0 | 325.9s | — |
| 2026-11-26T22:54:52.662Z | 81d775ab302c | yes | Mac14,10 | 27.2.1 | batch | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e | FAIL | 15/1/1 | 1248.6s | — |
| 2026-11-26T23:03:39.171Z | 89d8dd080947 | no | Mac14,10 | 27.2.1 | full | partial | containers | PASS | 9/0/0 | 523.6s | — |
| 2026-11-26T23:09:22.914Z | f9db8e3bbd8c | no | Mac14,10 | 27.2.1 | full | partial | gui | PASS | 9/0/0 | 343.6s | — |
| 2026-11-26T23:31:03.686Z | f9db8e3bbd8c | no | Mac14,10 | 27.2.1 | batch | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e | PASS | 17/0/0 | 1299.3s | — |
| 2026-11-26T23:39:38.273Z | f9db8e3bbd8c | no | Mac14,10 | 27.2.1 | change | default | containers, gui, native, newsdigest, os, runtimes, services | PASS | 14/0/1 | 514.5s | — |
| 2026-11-26T23:45:41.330Z | c518f9c8735a | no | Mac14,10 | 27.2.1 | change | default | static | PASS | 4/0/0 | 3.1s | — |
| 2026-11-27T00:33:23.661Z | 3f7001a8059f | no | Mac14,10 | 27.2.1 | change | partial | os | FAIL | 7/1/1 | 210.8s | smoke-archive-check from ~/homebox-images/homebox-smoke-check-20261126.hbimg.gz (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-27T00:41:21.693Z | d703e35e795d | no | Mac14,10 | 27.2.1 | change | partial | os | PASS | 9/0/0 | 390.3s | smoke-archive-check from ~/homebox-images/homebox-smoke-check-20261126.hbimg.gz (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-27T01:30:52.125Z | 8659f1cbbb32 | no | Mac14,10 | 27.2.1 | change | partial | os, runtimes, doctor | PASS | 11/0/0 | 3338.7s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-27T01:42:11.355Z | 53105a9b2018 | no | Mac14,10 | 27.2.1 | change | partial | os, runtimes, doctor | PASS | 11/0/0 | 655.0s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-27T01:56:12.283Z | 6c63fbcdb82c | no | Mac14,10 | 27.2.1 | change | partial | os, runtimes, doctor | PASS | 11/0/0 | 685.4s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-27T01:59:31.519Z | be456086706d | no | Mac14,10 | 27.2.1 | change | partial | os, runtimes, doctor | PASS | 11/0/0 | 198.4s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-27T02:04:12.995Z | be456086706d | yes | Mac14,10 | 27.2.1 | change | default | doctor | PASS | 9/0/0 | 160.1s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-27T02:05:26.732Z | be456086706d | no | Mac14,10 | 27.2.1 | change | partial | doctor | PASS | 9/0/0 | 355.1s | homebox-smoke from ~/homebox-images/homebox-smoke-check-20261126.hbimg.gz (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T03:30:51.775Z | adfe886abda3 | no | Mac14,10 | 27.2.1 | change | default | static | PASS | 4/0/0 | 9.2s | — |
| 2026-11-28T04:36:03.035Z | e0ddbedfe4ec | no | Mac14,10 | 27.2.1 | full | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e, desktop | FAIL | 20/3/1 | 2504.2s | fresh |
| 2026-11-28T05:02:15.801Z | 1de9a8577d20 | no | Mac14,10 | 27.2.1 | full | partial | desktop | FAIL | 13/2/0 | 355.9s | fresh |
| 2026-11-28T05:26:22.303Z | 07937da1852f | no | Mac14,10 | 27.2.1 | change | default | os, runtimes, doctor, e2e | PASS | 12/0/0 | 1066.3s | fresh |
| 2026-11-28T05:42:01.904Z | e26dc3c492f3 | no | Mac14,10 | 27.2.1 | change | partial | desktop | FAIL | 10/1/0 | 191.3s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
