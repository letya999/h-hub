# spawn-controller

## Purpose

The only service that touches containers: it owns spaces, compiles
minimal passports into the effective spec with one digest, and drives
the Runtime and Worker machines through the Docker Engine API.

## Zone and neighbors

Core zone, plus it launches space-zone containers. Called by `hubctl`
(owner-scoped space ops) and task-engine (`ResolveSpec`, `EnsureRuntime`,
`ReleaseRuntime`). Calls task-engine back only via edges declared in
the descriptor; no service calls out to the model — the space-zone
network edge reaches `scripted-model` per the models passport.
Cross-owner space reads and destroys are refused
([TestJobToAnswer](../../test/e2e/lifecycle/e2e_test.go)).

## Owns

`spaces`, effective-spec digests, `runtimes` and the `intents`
worker-intent registry in its own database; the Docker socket (the
only holder); the Runtime FSM; rendered harness configs under the
`hhub-render` volume; the watchdog tick row in `fsm`.

The controller renders the combined layer skill showcase on the
read-only mount. It checks the complete candidate body before replacing
an agent's Markdown skill. [Skill boundary tests](internal/svc/skills_test.go).

Worker claims bind the authenticated tenant, space, profile and
passport digest. Launch reads the installed image ID pinned at controller
startup and the workspace layout from the owned passport.
[Worker lifecycle scenario](../../test/e2e/lifecycle/worker_test.go).

## Contracts

Implements `contracts/spawn/v1` (`CreateSpace`, `DestroySpace`,
`GetSpace`, `ResolveSpec`, `EnsureRuntime`, `ReleaseRuntime`). Fake at
`contracts/spawn/v1/fake.go`.
Tool-hub reads the workspace skill inventory through `SkillInventory`.
[Skill scenario](../../test/e2e/channel/skills_test.go).

Tool-hub claims a worker through `ClaimWorker` and changes its lease
through `WorkerLease`. Cellinit serves the direct command call on its
fixed loopback port using a transferred TCP socket.
[Worker lifecycle scenario](../../test/e2e/lifecycle/worker_test.go).

## Does not do

No jobs, no model calls, no identity issuance. Never hands the Docker
socket or a database route to a space-zone container; never starts a
container without an `intents` row committed first
([`TestRuntimeIntent`](../../test/e2e/lifecycle/e2e_test.go),
`adr.spawn-runtime.intent-first`).

## Machines

Runtime: `absent -> starting -> warm -> busy -> draining -> absent`
plus `failed`, `quarantined`, `missing` (`LongLived`). `warm` carries a
15-minute idle TTL, `busy` re-arms through work, `draining` waits for
runs to settle. Sweeper and watchdog run in-process.

Worker generations start under a committed intent, move through
renewable leases and idle expiry, and stop through one Inspect rule.
An unconfirmed stop quarantines the key and keeps its socket directory
assigned. The janitor checks ownership labels for unregistered workers.
[Quarantine test](internal/svc/worker_db_test.go).
[Orphan scenario](../../test/e2e/lifecycle/orphan_worker_test.go).

## Config

`HHUB_SUB`, `HHUB_DSN`, `HHUB_KEYS`, `HHUB_RUNTIME_IMAGE`,
`HHUB_DOCKER_SOCK`, `HHUB_TASK_URL`, `HHUB_MODEL_URL`,
`HHUB_RENDER_VOL`, `HHUB_RENDER_DIR`, `HHUB_NETWORK`; loaded once in
`internal/config`.

## Runbook

Starts with `just stack`; needs the Docker socket. Healthy = `/healthz`
with a fresh watchdog age. If `EnsureRuntime` never reports `ready`,
inspect `fsm.machines` for a runtime wedged in `starting` and the
container logs of `hhub-rt-*`; check `intents` for the last action.
`hubctl user runtime-drain <owner> <space>` requests removal without
deleting the space ([TestHubctlUnblocksJobAndRuntime](../../test/e2e/lifecycle/wedge_unblock_test.go)).
Docker inspect, removal and orphan scans are confined to this installation's
network ([TestDockerRefusesForeignNetwork](internal/backend/docker_test.go)).
