# Phase 5 report — released as v0.0.5

## What passed

- Published `main` and tags `v0.0.1`–`v0.0.5` at
  [letya999/h-hub](https://github.com/letya999/h-hub). The first green
  [CI run](https://github.com/letya999/h-hub/actions/runs/45123645133)
  preceded branch protection. `main` now requires a green `check` status,
  a PR and linear history; force pushes are disabled.
- Removed **108** `hhub-rt-*` containers: 95 running and 13 stopped when
  cleanup ran. Their writable layers accounted for **1,769,472 bytes**;
  Docker's container total dropped from 1.888 MB to 118.8 kB. No image
  or volume was exclusively held by them, so none was removed.
- Host backups and installation secrets are outside the repository at
  `C:/Users/User/a_projects/h-hub-data`. Full-history gitleaks and
  trufflehog scans passed after removing a leaked test fixture from all
  history; authorship was retained. AGPL-3.0-only and third-party notices
  are in the published repository.
- The reverse [Docker orphan test](../../test/e2e/orphan_test.go) failed
  before the janitor and passed afterward. A separate
  [deadline test](../../test/e2e/runtime_sweep_test.go) found the main
  cause of the 108 old containers: runtime deadlines were never swept.
  Both paths now remove containers, and e2e/load tests destroy their
  provisioned spaces. A fresh post-load inspection found zero runtimes.
- [Stack restart e2e](../../test/e2e/e2e_test.go) now includes
  `channel-hub` and completes a new mock message-to-answer path after
  all four services and Postgres restart; cancelling the settled job
  is refused. The full e2e passed in **70.317 s** after Docker network
  confinement, with this negative check; local `just check` passed.
  The three-user load passed
  twice after cleanup: **36 messages**, **0 errors**, **7.6 s** and
  **8.5 s**, against the prior **26.9 s** outlier. This is correlation,
  not proof that orphan containers alone caused the outlier.
- A later load exposed a separate dispatch race: one runtime reported
  its final result before task-engine moved the job from `admitted` to
  `running`. The result was refused and the load timed out after
  **311.6 s**. The [regression](../../services/task-engine/internal/svc/dispatch_race_db_test.go)
  failed before the fix and passed after moving that transition into
  the run-binding transaction. Two new 36-message runs passed with
  **0 errors** in **9.0 s** and **8.7 s**. Cleanup again left zero
  `hhub-rt-*` containers.
- [Sweep-lag e2e](../../test/e2e/sweep_lag_test.go) held a queued job,
  moved its deadline one minute into the past, and received its exact
  operator alert. The threshold is 30 seconds. Task-engine records
  the alert and outbox intent in one transaction; channel-hub delivers
  it once by key. A forged operator call to the service alert endpoint
  is refused ([contract e2e](../../test/e2e/operator_alert_test.go)).
- [Watchdog e2e](../../test/e2e/watchdog_stale_test.go) froze the spawn
  tick: spawn-controller returned unhealthy, an unauthenticated status
  read was refused, and channel-hub sent the operator alert through its
  direct delivery outbox. A stable tick value deduplicates each incident.
- [Exit-command e2e](../../test/e2e/wedge_unblock_test.go) applied
  `hubctl job cancel` to a held job and `hubctl user runtime-drain` to a
  live runtime, then saw the runtime reach `absent` and its container
  disappear. DB contract scenarios cover every non-final job and
  runtime state and refuse a foreign owner's request
  ([job](../../services/task-engine/internal/svc/wedge_db_test.go),
  [runtime](../../services/spawn-controller/internal/svc/drain_runtime_db_test.go)).
- The [restore drill](../../tools/backup/restore-drill.sh) restored nine
  databases, decrypted the backed-up issuer key, created and linked a
  fresh user, and returned the scripted answer to a new message. Its
  databases, network and volumes were isolated. The restored
  spawn-controller shared the Docker Engine but could inspect and
  remove containers only on its own network
  ([boundary test](../../services/spawn-controller/internal/backend/docker_test.go)).
  No restored containers, volumes or runtime containers remained.
- Phase 5 adds the additive identity-hub migration
  `0006_link_codes_expiry.sql` and task-engine alert-intent table
  migration `0003_operator_alerts.sql`. The live identity database
  reports version **6**.
  The frozen v0.0.4 identity image was published by the
  [GHCR run](https://github.com/letya999/h-hub/actions/runs/45127280913)
  and the first smoke pulled its pinned digest. After restoring a
  version-6 backup, that exact image exited with `migrations: invalid:
  err.migration`. The owner approved a forward-readable **v0.0.4.1**
  patch baseline. Its [PR](https://github.com/letya999/h-hub/pull/6)
  passed CI, and the [image run](https://github.com/letya999/h-hub/actions/runs/45157193310)
  published six images. The smoke now pins identity-hub digest
  `sha256:b3c0e0c6a549cdb9c32fcad5d096bdb431a2f5f055c7aed907a2dd9de0b4e02d`.
  A fresh nine-database backup restored on schema 0.0.5; the published
  v0.0.4.1 binary started, its issuer key decrypted, and a new message
  got an answer. The local N-1 smoke and the
  [release CI smoke](https://github.com/letya999/h-hub/actions/runs/45170610093)
  passed. PostgreSQL readiness is checked over TCP so the drill cannot
  mistake the temporary initialization server for the final server
  ([regression](../../tools/backup/postgres_ready_test.go)).
- The [invariant register](../invariants.md) lists all **47** declared IDs:
  **27 proved** and **20 deferred**. For each proved ID, `just check`
  now runs the named scenario without test caching and requires both a
  passed test and an exercised `testkit.Deny` refusal. Broken-register
  and label-only fixtures fail the gate. The full local `just check`,
  including real-container e2e, passed. `sys.tenant-isolation` now
  requires owner A's token to receive a refusal when reading B's
  outbox; an empty list does not count.

## `sys.*` matrix

`Proved` means the cited scenario ran and checks the stated boundary.
`Deferred` identifies an absent owner and destination phase. Partial
evidence does not count as `Proved`.

| ID | Status | Owner / evidence or missing proof |
| --- | --- | --- |
| `sys.tenant-isolation` | Proved | Task/channel/spawn e2e refuses owner A reading B's data. |
| `sys.forged-token` | Proved | Cross-owner and wrong-key e2e return an error. |
| `sys.secret-leak` | Proved | Dump, log, inspect and answer checks refuse plaintext exposure. |
| `sys.kill-uncertain` | Proved | SIGKILL/outbox scenarios keep uncertain work from blind retry. |
| `sys.store-missing` | Proved | Missing database refuses service startup. |
| `sys.stack-restart` | Proved | Four services plus Postgres restart; fresh message gets an answer and settled job rejects cancellation. |
| `sys.late-receipt` | Proved | Settled job remains settled after a late receipt. |
| `sys.seam-onesided` | Deferred to phases 10/18 | Channel adapter and tool seam owners do not exist. |
| `sys.sweep-lag` | Proved | Task-engine: one-minute overdue job creates a durable operator alert; operator token cannot forge it. |
| `sys.wedge-unblock` | Proved | CLI exits a held job and live runtime; contract scenarios cover every non-final state and refuse cross-owner exit. |
| `sys.schema-version` | Proved | Future spec version is refused; additive migration is checked. |
| `sys.worker-intent` | Deferred to phase 13 | Spawn-controller runtime orphan scenario passes; worker owner is absent. |
| `sys.space-no-db` | Deferred to phase 13 | Runtime inspect has no DB credential/socket; worker owner is absent. |
| `sys.seal-at-rest` | Deferred to phase 7 | Issuer seed is sealed; credential-broker secret-value owner is absent. |
| `sys.watchdog-stale` | Proved | Stale tick makes spawn-controller unhealthy; channel-hub sends a direct operator alert, and an unauthenticated status read is refused. |
| `sys.service-edge` | Proved | Wrong operation or absent token is refused. |
| `sys.worker-tenant` | Deferred to phase 13 | Worker owner is absent. |
| `sys.worker-stop-confirmed` | Deferred to phase 13 | Worker owner is absent. |
| `sys.package-build` | Deferred to phase 17 | Package owner is absent. |
| `sys.recipe-hosts` | Deferred to phase 17 | Recipe owner is absent. |

The `space-no-db` row is limited to the runtime in this phase. A phase-13
worker scenario must complete the broader claim before release of that
worker. The `seal-at-rest` row remains deferred for the same reason.

## Release validation

- The [phase-5 PR](https://github.com/letya999/h-hub/pull/5) passed
  [Ubuntu CI](https://github.com/letya999/h-hub/actions/runs/45164496016):
  `just check` and the published-image N-1 smoke. A separate
  [PostgreSQL readiness PR](https://github.com/letya999/h-hub/pull/7)
  passed its [CI](https://github.com/letya999/h-hub/actions/runs/45169804160)
  after a real regression test failed on the temporary socket server.
  The [release commit on main](https://github.com/letya999/h-hub/actions/runs/45170610093)
  passed both gates again. [Tag v0.0.5](https://github.com/letya999/h-hub/tree/v0.0.5)
  points to that green commit.
- The [GHCR image run](https://github.com/letya999/h-hub/actions/runs/45171339057)
  published all six `v0.0.5` service and CLI images and printed a digest
  for each. The [manually dispatched nightly](https://github.com/letya999/h-hub/actions/runs/45171353174)
  passed `Check`, a 36-message load, backup, full nine-database restore,
  message-to-answer probe, and the published-digest N-1 smoke. The load
  took **7.4 s** with **0 errors**; the whole nightly took **5m28s**.
- An [intentional failure run](https://github.com/letya999/h-hub/actions/runs/45172856579)
  on a separate branch failed before build, then its `Alert on failure`
  step created [issue #8](https://github.com/letya999/h-hub/issues/8)
  with the exact run URL. The branch was deleted and the probe issue
  closed. The successful nightly did not create an issue.

## Remaining limits

- The original v0.0.4 image is immutable and rejects migration 6. The
  owner-approved v0.0.4.1 digest is the tested rollback baseline. The
  smoke runs the published **identity-hub** binary on the 0.0.5 backup;
  it does not run the full previous-version stack. In particular, the
  old task-engine image has not been tested independently against its
  new alert-intent table.
- The invariant register defers **12 ADR claims** without an executed
  refusal at their boundary. The **8 deferred `sys.*` IDs** await the
  owners and phases named in the matrix. Partial tests are not proof.
- Cleanup and the dispatch-race fix reduced repeated 36-message loads
  to 7.4–9.0 s, but the earlier 26.9 s outlier cannot be attributed to
  orphan containers alone from these measurements.

## Redesign signals

- Production Go lines excluding tests: spawn-controller **1,417/8,000**,
  identity-hub **798/3,000**, task-engine **1,598/4,000**,
  channel-hub **872/4,000**, harness-runtime **395/6,000**.
  No size trigger fired. Keep the four deployed product services plus
  the per-space harness runtime.
- Mandatory e2e: **70.317 s** against the 10-minute limit. Full local
  `just check` with executed trace proofs and real-container e2e took
  **263.9 s**, under five minutes. The release CI completed in **4m14s**;
  the manually triggered nightly completed in **5m28s**. No duration
  trigger fired.
- Passing three-user loads after cleanup and dispatch-race repair took
  **7.4–9.0 s** for 36 messages, compared with the earlier **26.9 s**
  outlier. The separate early-result race once caused a **311.6 s**
  failed load; its regression and subsequent loads passed.
