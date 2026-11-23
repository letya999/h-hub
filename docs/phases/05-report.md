# Phase 5 report — release blocked by frozen N-1 image

## What passed

- Published `main` and tags `v0.0.1`–`v0.0.4` at
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
  is refused. The full e2e passed in **61.826 s** after the runtime
  exit command was added, with this negative
  check; local `just check` passed. The three-user load passed
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
- Phase 5 adds the additive identity-hub migration
  `0006_link_codes_expiry.sql` and task-engine alert-intent table
  migration `0003_operator_alerts.sql`. The live identity database
  reports version **6**.
  The frozen v0.0.4 identity image was published by the
  [GHCR run](https://github.com/letya999/h-hub/actions/runs/45127280913)
  and the smoke now pulls its pinned digest. After restoring a version-6
  backup, that exact image exits with `migrations: invalid:
  err.migration`. The smoke correctly fails. The v0.0.4 binary rejects
  migration numbers above its embedded list; a phase-5 change cannot
  alter that published binary.

## `sys.*` matrix

`Proved` means the cited scenario ran and checks the stated boundary.
`Deferred` identifies an absent owner and destination phase. `Blocked`
means a phase-5 owner exists but the required proof is still missing;
it does **not** count as a passing status.

| ID | Status | Owner / evidence or missing proof |
| --- | --- | --- |
| `tenant-isolation` | Proved | Task/channel/spawn e2e refuses owner A reading B's data. |
| `forged-token` | Proved | Cross-owner and wrong-key e2e return an error. |
| `secret-leak` | Proved | Dump, log, inspect and answer checks refuse plaintext exposure. |
| `kill-uncertain` | Proved | SIGKILL/outbox scenarios keep uncertain work from blind retry. |
| `store-missing` | Proved | Missing database refuses service startup. |
| `stack-restart` | Proved | Four services plus Postgres restart; fresh message gets an answer and settled job rejects cancellation. |
| `late-receipt` | Proved | Settled job remains settled after a late receipt. |
| `seam-onesided` | Deferred to phases 10/18 | Channel adapter and tool seam owners do not exist. |
| `sweep-lag` | Proved | Task-engine: one-minute overdue job creates a durable operator alert; operator token cannot forge it. |
| `wedge-unblock` | Proved | CLI exits a held job and live runtime; contract scenarios cover every non-final state and refuse cross-owner exit. |
| `schema-version` | Proved | Future spec version is refused; additive migration is checked. |
| `worker-intent` | Deferred to phase 13 | Spawn-controller runtime orphan scenario passes; worker owner is absent. |
| `space-no-db` | Deferred to phase 13 | Runtime inspect has no DB credential/socket; worker owner is absent. |
| `seal-at-rest` | Deferred to phase 7 | Issuer seed is sealed; credential-broker secret-value owner is absent. |
| `watchdog-stale` | Proved | Stale tick makes spawn-controller unhealthy; channel-hub sends a direct operator alert, and an unauthenticated status read is refused. |
| `service-edge` | Proved | Wrong operation or absent token is refused. |
| `worker-tenant` | Deferred to phase 13 | Worker owner is absent. |
| `worker-stop-confirmed` | Deferred to phase 13 | Worker owner is absent. |
| `package-build` | Deferred to phase 17 | Package owner is absent. |
| `recipe-hosts` | Deferred to phase 17 | Recipe owner is absent. |

The `space-no-db` row is limited to the runtime in this phase. A phase-13
worker scenario must complete the broader claim before release of that
worker. The `seal-at-rest` row remains deferred for the same reason.

## Remaining work and release decision

- The rollback gate is red on the published v0.0.4 digest and version-6
  schema. Do not substitute a locally rebuilt image or hide the migration.
  The old task-engine binary also predates its new migration; the
  current smoke exercises the published identity image, not a full
  previous-version stack.
- The restore drill starts four services and decrypts the backed-up
  issuer key, but does not yet prove message-to-answer. Its isolated
  spawn-controller has no Docker socket, so it cannot start a runtime;
  attaching the live socket would let the restored janitor affect live
  containers. This needs an isolated Docker engine for a safe end-to-end
  recovery drill.
- The trace gate still checks links, not
  execution plus negative assertions; the invariant registry and new
  gate are not complete.
- The first [phase-5 PR CI](https://github.com/letya999/h-hub/actions/runs/45130542931)
  passed `just check` but stopped before the image smoke because the
  runner could not find nested `just init`. A regression test failed
  before the workflow PATH fix and passed afterward. The updated PR
  reached the published-image incompatibility in the
  [second CI run](https://github.com/letya999/h-hub/actions/runs/45132085394):
  `just check` passed, then the v0.0.4 identity binary rejected migration 6.
- The [latest PR CI](https://github.com/letya999/h-hub/actions/runs/45139439017)
  also passed Ubuntu `just check` and failed only at the pinned v0.0.4
  image smoke with `migrations: invalid: err.migration`.
- Phase 5 is **not ready for merge or `v0.0.5`**. Manual dispatch of
  `nightly.yml` returned HTTP 404 because the workflow is not on the
  default branch. The synthetic failure-to-issue check cannot run yet.
  No skipped check is counted as passing.

## Redesign signals

- Production Go lines excluding tests: spawn-controller **1,417/8,000**,
  identity-hub **798/3,000**, task-engine **1,598/4,000**,
  channel-hub **872/4,000**, harness-runtime **395/6,000**.
  No size trigger fired. Keep the four deployed product services plus
  the per-space harness runtime.
- Mandatory e2e: **61.826 s** against 10 minutes. Local `just check`
  passed against the 5-minute criterion. Passing three-user loads are
  **7.6–9.0 s** for 36 messages; the earlier **26.9 s** remains an
  unexplained outlier, though runtime leakage was present then. A
  separate early-result race caused one later 311.6 s failed run;
  its focused regression and two post-fix load runs are green.
- The release blocker is compatibility with the immutable previous
  binary, not service size or test duration.
