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
| 2026-11-28T05:54:43.253Z | c714943edddd | yes | Mac14,10 | 27.2.1 | full | partial | desktop | FAIL | 10/2/0 | 321.8s | fresh |
| 2026-11-28T06:22:46.829Z | e7893e83d97a | no | Mac14,10 | 27.2.1 | full | partial | desktop | FAIL | 14/1/0 | 417.6s | fresh |
| 2026-11-28T06:26:07.476Z | f4f19f6b665d | no | Mac14,10 | 27.2.1 | change | default | desktop | FAIL | 10/1/0 | 193.2s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T06:41:47.339Z | 063afafa5505 | no | Mac14,10 | 27.2.1 | change | default | desktop | PASS | 11/0/0 | 183.5s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T06:47:26.810Z | f3eecf9bc401 | no | Mac14,10 | 27.2.1 | change | default | static | PASS | 5/0/0 | 12.1s | — |
| 2026-11-28T06:48:41.524Z | 2af577cac94d | no | MS-7D31 | Ubuntu 26.04.1 LTS | full | partial | desktop | PASS | 14/0/1 | 322.2s | fresh |
| 2026-11-28T06:49:46.265Z | 063afafa5505 | no | Mac14,10 | 27.2.1 | full | partial | desktop | PASS | 15/0/0 | 478.9s | fresh |
| 2026-11-28T06:52:16.674Z | c714943edddd | yes | Mac14,10 | 27.2.1 | batch | partial | os, doctor | PASS | 10/0/0 | 411.4s | fresh |
| 2026-11-28T06:52:30.246Z | 071b81581eaa | no | Mac14,10 | 27.2.1 | change | default | static | PASS | 5/0/0 | 7.7s | — |
| 2026-11-28T06:54:15.000Z | f867ec195605 | no | Mac14,10 | 27.2.1 | change | default | os, runtimes, doctor | PASS | 11/0/0 | 548.5s | fresh |
| 2026-11-28T06:55:41.618Z | 063afafa5505 | no | Mac14,10 | 27.2.1 | full | partial | desktop | FAIL | 14/1/0 | 355.3s | fresh |
| 2026-11-28T07:01:20.750Z | f867ec195605 | no | Mac14,10 | 27.2.1 | batch | partial | os, doctor | PASS | 9/0/0 | 416.6s | fresh |
| 2026-11-28T07:01:54.442Z | 5debd0e0a5c3 | no | Mac14,10 | 27.2.1 | change | default | static | PASS | 4/0/0 | 6.5s | — |
| 2026-11-28T07:02:47.750Z | 928541e1e395 | no | Mac14,10 | 27.2.1 | change | default | static | PASS | 5/0/0 | 7.3s | — |
| 2026-11-28T07:03:02.414Z | 0fa36e510d7b | no | Mac14,10 | 27.2.1 | full | partial | desktop | PASS | 15/0/0 | 388.7s | fresh |
| 2026-11-28T07:09:32.813Z | 601d1b50aae6 | no | Mac14,10 | 27.2.1 | change | default | doctor, e2e | PASS | 10/0/0 | 1033.7s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T07:09:45.465Z | 0fa36e510d7b | no | Mac14,10 | 27.2.1 | full | partial | desktop | PASS | 15/0/0 | 403.0s | fresh |
| 2026-11-28T07:16:48.094Z | 0fa36e510d7b | no | Mac14,10 | 27.2.1 | full | partial | desktop | PASS | 15/0/0 | 422.5s | fresh |
| 2026-11-28T07:17:40.952Z | 765ff4798484 | no | MS-7D31 | Ubuntu 26.04.1 LTS | full | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e, desktop | PASS | 23/0/1 | 1674.9s | homebox-smoke from ~/.homebox (disk SHA-256 424f1ab36dfd5d96801e3929a4897fee8dfd82a628efb38de8ede13e55e7e508) |
| 2026-11-28T07:19:56.190Z | 0fa36e510d7b | no | Mac14,10 | 27.2.1 | change | default | desktop | PASS | 11/0/0 | 188.0s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T07:22:48.304Z | ee08b9eed107 | no | Mac14,10 | 27.2.1 | batch | partial | e2e | PASS | 6/0/0 | 795.4s | — |
| 2026-11-28T07:23:40.128Z | 49c67f72ebf9 | no | Mac14,10 | 27.2.1 | change | default | desktop | PASS | 12/0/0 | 197.3s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T07:27:59.016Z | b0ce3cf83b46 | no | Mac14,10 | 27.2.1 | batch | partial | os | PASS | 10/0/0 | 338.0s | fresh |
| 2026-11-28T07:28:26.166Z | 3ad5cea4d641 | no | Mac14,10 | 27.2.1 | batch | partial | os, doctor | PASS | 11/0/0 | 333.9s | fresh |
| 2026-11-28T07:30:33.488Z | 49c67f72ebf9 | no | Mac14,10 | 27.2.1 | full | partial | desktop | PASS | 16/0/0 | 413.3s | fresh |
| 2026-11-28T07:33:12.327Z | 765ff4798484 | no | MS-7D31 | Ubuntu 26.04.1 LTS | change | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e, desktop | PASS | 23/0/0 | 905.6s | homebox-smoke from ~/.homebox (disk SHA-256 424f1ab36dfd5d96801e3929a4897fee8dfd82a628efb38de8ede13e55e7e508) |
| 2026-11-28T07:34:28.701Z | 92fe09f48cdd | no | Mac14,10 | 27.2.1 | change | default | desktop | PASS | 12/0/0 | 203.6s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T07:46:00.064Z | c90c74810834 | no | Mac14,10 | 27.2.1 | batch | partial | os, doctor | PASS | 11/0/0 | 351.3s | fresh |
| 2026-11-28T07:48:04.079Z | 765ff4798484 | no | MS-7D31 | Ubuntu 26.04.1 LTS | batch | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e | PASS | 17/0/0 | 891.7s | fresh |
| 2026-11-28T08:03:37.754Z | 6bc4a86c50a3 | no | MS-7D31 | Ubuntu 26.04.1 LTS | full | partial | services, doctor, e2e, desktop | PASS | 18/0/1 | 864.5s | homebox-smoke from ~/.homebox (disk SHA-256 424f1ab36dfd5d96801e3929a4897fee8dfd82a628efb38de8ede13e55e7e508) |
| 2026-11-28T08:12:56.338Z | 77154b81d5f9 | no | Mac14,10 | 27.2.1 | batch | partial | e2e | PASS | 6/0/0 | 731.7s | — |
| 2026-11-28T08:18:12.309Z | cd361fc2635c | no | Mac14,10 | 27.2.1 | change | default | containers, gui, native, newsdigest, os, runtimes, services, doctor, e2e, desktop | FAIL | 19/1/1 | 766.8s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T09:03:10.856Z | 5be7c55faf1f | no | MS-7D31 | Ubuntu 26.04.1 LTS | change | default | doctor, e2e | PASS | 11/0/0 | 717.9s | homebox-smoke from ~/.homebox (disk SHA-256 424f1ab36dfd5d96801e3929a4897fee8dfd82a628efb38de8ede13e55e7e508) |
| 2026-11-28T09:14:26.827Z | 5be7c55faf1f | no | MS-7D31 | Ubuntu 26.04.1 LTS | batch | partial | os, doctor, e2e | PASS | 12/0/0 | 675.9s | fresh |
| 2026-11-28T08:18:31.301Z | 77154b81d5f9 | no | Mac14,10 | 27.2.1 | change | default | os, runtimes, doctor | PASS | 12/0/0 | 334.8s | fresh |
| 2026-11-28T08:24:55.875Z | 1d7b34045c1e | no | Mac14,10 | 27.2.1 | change | default | os, runtimes, doctor | PASS | 12/0/0 | 318.8s | fresh |
| 2026-11-28T08:37:24.579Z | 1d7b34045c1e | no | Mac14,10 | 27.2.1 | batch | partial | e2e | PASS | 6/0/0 | 748.5s | — |
| 2026-11-28T07:48:48.235Z | 7f3fa4a991cf | no | Mac14,10 | 27.2.1 | change | default | doctor, e2e | FAIL | 10/1/0 | 803.5s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T08:07:47.348Z | 0626d22750ea | no | Mac14,10 | 27.2.1 | batch | partial | os, doctor, e2e | PASS | 12/0/0 | 1132.2s | fresh |
| 2026-11-28T08:17:54.136Z | 7d2dc7f252c8 | no | Mac14,10 | 27.2.1 | change | default | doctor, e2e | FAIL | 10/1/0 | 585.4s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T08:33:24.392Z | db5e415ffab2 | no | Mac14,10 | 27.2.1 | change | default | doctor, e2e | PASS | 11/0/0 | 916.7s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T08:49:16.914Z | 6e10b577bd50 | no | Mac14,10 | 27.2.1 | change | default | doctor, e2e | PASS | 11/0/0 | 910.1s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
| 2026-11-28T08:52:27.995Z | 9aba63bdcca5 | no | Mac14,10 | 27.2.1 | change | partial | doctor | PASS | 10/0/0 | 170.7s | homebox-smoke from ~/.homebox (disk SHA-256 8ce466a4b8e0245c4c1b365237decca45acf25992f311bbe9c3011f601ac9cc0) |
