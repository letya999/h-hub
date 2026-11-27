# Phase 14 report

## Worker and terminal

- The controller uses one Worker machine and an installed, immutable
  image ID per executor profile. A claim binds tenant, space, profile
  and passport digest before recording its Docker intent. Claims count
  against the descriptor limits of two per tenant and eight overall.
  `just stack` reloads the controller after building the executor image
  so the active profile resolves the newly installed image ID.
  [Registry test](../../services/spawn-controller/internal/svc/worker_db_test.go).
- Cellinit runs as static PID 1. Docker exec stdin installs its token
  into tmpfs. Cellinit transfers a connected TCP socket to tool-hub
  through an authenticated Unix socket exchange. HTTP still enters on
  the fixed loopback port in the worker's `none` network. Tool-hub
  provisions the socket directories in its transport volume, and each
  worker mounts only its reserved directory.
  [Docker network isolation](https://docs.docker.com/engine/network/drivers/none/).
  [Linux socket descriptor transfer](https://man7.org/linux/man-pages/man7/unix.7.html).
- Workspace mounts come from the effective passport under the verified
  space ID. Docker enforces the non-root UID, read-only root filesystem,
  no-exec tmpfs, dropped capabilities and profile and passport budgets.
  An observed absence releases the transport slot. If that observation
  fails, the key stays quarantined. Janitor removes unregistered workers
  with the installation label.
  [Real lifecycle scenario](../../test/e2e/lifecycle/worker_test.go).
  [Orphan scenario](../../test/e2e/lifecycle/orphan_worker_test.go).
- New mounts wait while a worker from another passport revision still
  owns paths in the space. Mount preparation refuses symlinks before
  Docker sees the workspace path.
  [Revision test](../../services/spawn-controller/internal/svc/worker_db_test.go#TestRuntimeWaitsForOlderWorkerMounts).
  [Symlink test](../../services/spawn-controller/internal/svc/workspace_test.go#TestWorkspaceMountRefusesAnotherSpacesSymlink).
- The terminal tool uses the effective grant and approvals mode. It
  records a stable call ID before dispatch. A missing response leaves
  an uncertain outcome, and a repeated call ID cannot dispatch twice.
  The mock channel scenario exercised authorized execution, truncation,
  a lasting workspace write and revocation.
  [Terminal scenario](../../test/e2e/channel/terminal_test.go).
  [Kill and restart scenarios](../../test/e2e/disrupt/terminal_faults_test.go).

## Proof matrix

| ID | Executed evidence |
| --- | --- |
| `sys.worker-intent` | The controller refused a launch delivery without a registered worker. A real claim committed its intent before Docker creation, and reapplying the same Docker intent returned the same container. [Lifecycle](../../test/e2e/lifecycle/worker_test.go), [Docker boot](../../services/spawn-controller/internal/backend/cellinit_e2e_test.go). |
| `sys.worker-tenant` | A foreign owner could not claim the space. The real container mounted the owned workspace subpaths and its reserved transport directory. [Lifecycle](../../test/e2e/lifecycle/worker_test.go). |
| `sys.worker-stop-confirmed` | A real Docker identity mismatch left the live worker quarantined and the key closed. After the obstruction cleared, Inspect confirmed absence and released the slot. The deadline test checked that expiry queues the exact generation for removal. [Quarantine](../../test/e2e/lifecycle/worker_quarantine_test.go), [Deadline](../../services/spawn-controller/internal/svc/worker_db_test.go#TestWorkerDeadlineQueuesItsGenerationForStop). |
| `sys.space-no-db` | A real sandbox command could not connect to the stack's Postgres address. Inspect showed only the `none` network, the executable path in the environment and the expected workspace and transport mounts. [Lifecycle](../../test/e2e/lifecycle/worker_test.go). |
| `cap.terminal.exec` | The mock channel drove a terminal write and read it through the file tool. A later command reported truncation. Revoking the grant removed the tool and refused an invocation. [Terminal](../../test/e2e/channel/terminal_test.go). |
| `sys.kill-uncertain` | The fault scenario kills a worker after observing the running command's marker and checks the uncertain response and the count of dispatch attempts. [Fault scenarios](../../test/e2e/disrupt/terminal_faults_test.go). |

## Timings

| Measurement | Result |
| --- | ---: |
| Five cold starts from claim to first direct command response | 0.771, 0.776, 0.658, 0.689, 0.505 s |
| Median claim-to-response | **0.689 s** |
| Median alongside the real quarantine scenario | **0.713 s** |
| Median in the full local check | **0.830 s** |
| Focused mock-channel terminal scenario | 32.59 s |
| Mock-channel terminal scenario in the full local check | 35.12 s |
| Focused worker kill and tool-hub restart | 8.00 s |
| Control group | 17 s |
| Real Hermes group, concurrent with control | 358 s |
| Disruption group | 43 s |
| Mandatory e2e | **401 s** |
| Full local `just check` | **588.47 s** |

The image was already installed for each cold start. The measured path
includes controller admission, Docker creation, authenticated readiness,
the connection handoff and a short real command. The limit is 1.5 s
for median cold start.
Docker reported 211 MiB for the terminal image.

## Release verification

The full local `just check` passed build, lint, vet, unit and database
tests, mandatory e2e, executed trace proofs, secret scanning and
descriptor validation. Mandatory e2e completed within the 600 s budget.
The full run exceeded the five-minute target tracked in Phase 12.
[Phase 12 timing follow-up](12-report.md).

[Release tree](https://github.com/letya999/h-hub/tree/v0.0.14) ·
[Nightly runs for the release tag](https://github.com/letya999/h-hub/actions/workflows/nightly.yml?query=branch%3Av0.0.14).

## Failures found during verification

- Deadline expiry reached PostgreSQL with an argument of the wrong
  type for the outbox payload. The regression now checks that the
  deadline queues a stop for the exact generation.
  [Deadline test](../../services/spawn-controller/internal/svc/worker_db_test.go#TestWorkerDeadlineQueuesItsGenerationForStop).
- Rebuilding only the worker image left the controller with the
  previous immutable image ID. Reloading the controller activates the
  installed revision before claims resume.
  [Stack recipe](../../justfile).

## Owner questions

The Phase 13 transport question is resolved by the authenticated
connection handoff described above. The decision still open is how to
bring the warm check below five minutes.
[Phase 12 timing follow-up](12-report.md).
