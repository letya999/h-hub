# Phase 5 report — foundation fault matrix (release blocked)

## Delivered and observed

- A late run receipt is retained after settlement without reopening the job.
  The database regression failed before the fix and passes now
  ([test](../../services/task-engine/internal/svc/late_receipt_db_test.go)).
- Runtime creation can replay after Docker created the named container but
  before the database recorded its ID. A mismatched image is refused. The
  Docker API test failed before the fix and passes now
  ([test](../../services/spawn-controller/internal/backend/docker_test.go)).
- New effective specs carry schema version 1; the reader accepts previous
  unversioned specs and refuses unknown future versions. The shared store
  accepts an additive newer migration prefix while still checking known
  hashes and gaps ([spec test](../../services/spawn-controller/internal/svc/spec_test.go),
  [store test](../../shared/store/store_db_test.go)).
- The seal test proves the stored issuer key opens with the host KEK, while
  the link code and signing seed are absent from the identity database dump,
  selected container logs and inspect output, and the mock answer
  ([test](../../test/e2e/seal_test.go)).
- `just backup` captured the host KEK, installation config, globals and all
  **9** non-template databases with checksums outside git. `just
  restore-drill` restored them into an isolated Postgres and started
  identity-hub with the backed-up KEK. `just nminus1-smoke` built the local
  `v0.0.4` identity source and started it against that restored backup.
- The latest `just load` passed: **36 messages**, **3 users**, **0 observed
  failures**, **26.9 s** from send to all answers (30.5 s test runtime).
  A prior run took 8.2 s for the same 36 messages
  ([test](../../test/e2e/load_test.go)).
- Local `just check` passed in about **119 s** (<5 min). Rebuilt-stack
  `just e2e` passed in **61.401 s** (<10 min), including a new
  missing-database startup refusal
  ([test](../../test/e2e/store_missing_test.go)). The separate nightly workflow
  runs check, stack, e2e, load, backup, restore and N-1 smoke, and attempts
  to create a GitHub issue when a run fails. It has not run remotely.

## Required `sys.*` matrix

| ID | Local evidence | Status |
| --- | --- | --- |
| `tenant-isolation` | Three-owner mock channel and job e2e | Passed |
| `forged-token` | Wrong-key and cross-owner e2e | Passed |
| `secret-leak` | Dump, log, inspect, answer checks | Passed |
| `kill-uncertain` | SIGKILL between FSM commit and effect; runtime kill e2e | Passed |
| `store-missing` | Missing database startup refusal e2e | Passed |
| `stack-restart` | Postgres and three services restarted; channel not checked | Partial |
| `late-receipt` | Settled job database regression | Passed |
| `seam-onesided` | Seam owner absent | Unproved |
| `sweep-lag` | Deadline sweep works; lag alert not tested | Unproved |
| `wedge-unblock` | No executed fault scenario | Unproved |
| `schema-version` | Effective spec reader and migration prefix tests only | Partial |
| `worker-intent` | Worker janitor owner absent; runtime intent is a separate invariant | Unproved |
| `space-no-db` | Runtime inspect passed; worker owner absent | Partial |
| `seal-at-rest` | Signing seed sealed; secret values owner absent | Partial |
| `watchdog-stale` | Tick age exists; no stale alert scenario | Unproved |
| `service-edge` | Wrong operation and missing token e2e | Passed |
| `worker-tenant` | Worker owner absent | Unproved |
| `worker-stop-confirmed` | Worker owner absent | Unproved |
| `package-build` | Package owner absent | Unproved |
| `recipe-hosts` | Recipe owner absent | Unproved |

The mock channel writes its send row and `ack` transition in one database
transaction. It has no external send/ack gap to kill; the Telegram adapter
will introduce that boundary. The actual Docker process-kill boundary is
still untested despite the replay test. No exception or gate threshold was
lowered to cover these gaps.

Three earlier trace links overstated coverage: missing config as
`sys.store-missing`, deadline sweep as `sys.sweep-lag`, and runtime intent
as `sys.worker-intent`. Their labels were removed. A new missing-database
test proves the first; the latter two remain unproved.
The current trace gate checks whether a linked ID is declared; it does not
verify that the linked test exercises the invariant. The audit found these
false proofs despite green gates. Strengthening that gate needs a separate
owner decision because several future owners have no runnable scenario yet.

## Rollback and restore limits

- The N-1 smoke used a locally built tag, not a published v0.0.4 image
  digest. Phase 5 adds no database migration, so this run does not prove
  that the frozen v0.0.4 binary reads a genuinely expanded v0.0.5 schema.
  The local tag is not on the remote while pushes are prohibited.
- Restore checks identity-hub startup and issuer decryption, not the full
  restored service stack or an answered message. The backup and restore
  scripts are locally green, but this is short of the phase criterion.
- Remote CI and the nightly failure alert have not been observed. The owner
  will judge a week of live nightlies separately.

## Redesign signals and recommendation

- Phase 5 changed production code in **2** service directories and no
  `contracts/*/v1` files. No feature crossed the three-service signal, and
  no service-pair contract changed in two consecutive phases.
- Production Go lines, excluding tests: spawn-controller **1,343/8,000**,
  identity-hub **798/3,000**, task-engine **1,510/4,000**,
  channel-hub **795/4,000**, harness-runtime **395/6,000**. None reaches
  70% of its blueprint budget. The phase added **0** contract or fake
  lines, so contract wrapping is below half of added code.
- Mandatory e2e **61.401 s** and local `just check` about **119 s** are
  below the 10- and 5-minute signals. CI timings are unavailable without
  a push. The phase-17 MCP template signal does not yet apply.
- The current running core is four product service binaries
  (identity-hub, channel-hub, task-engine, spawn-controller) plus the
  per-space harness runtime. The descriptor provisions 12 service roles;
  most later owners are not deployed. Keep this count for the next slice;
  add only an owner required by a proved end-to-end need. No service split
  is justified by the measured check and e2e times.
- A local `docker ps` after repeated acceptance runs showed over 60
  `hhub-rt-*` runtime containers. Tests provision new home spaces and do not
  clean them up. This can consume host resources and may explain the load
  variation; causality was not measured. Preserve these containers until
  their owning space lifecycle is reviewed.
- Existing spaces still pin runtime image IDs that may disappear after a
  rebuild. The restore drill also needs a full-stack continuation test.

## Release decision

The phase 5 readiness matrix is **not green**. The branch remains
`phase-05`; no merge to protected `main` and no `v0.0.5` tag were made.
All completed work is committed locally. A remote PR and CI run require a
push, which the owner explicitly excluded from this overnight task.
