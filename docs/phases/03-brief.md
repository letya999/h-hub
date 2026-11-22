# Phase 3 brief — first end-to-end path "task -> answer" (v0.0.3)

Source: h-hub-blueprint.md §2 (intro, boundary and machine rules), §2.1
spawn-controller, §2.3 task-engine, §2.11 harness-runtime, §3 passports,
§6.1 steps 4-13, §9 phase 3; enforcement.md sections 3-4;
owner-decisions.md — when a threshold or decision is disputed, do not
invent. Do not open or copy hermes-hub code.

Work branch: `phase-03`. Tag: `v0.0.3`.

## Goal

First end-to-end path "task -> answer" across three services: no channel,
no rights, no secrets.

## Work order

Contracts and fake identity first, then task-engine, then
spawn-controller, then harness-runtime, and the end-to-end test last.

## Scope

### spawn-controller (budget 8000 production lines)

- Space created from a layer template.
- Minimal passports `stack`, `config`, `models` compiled into one
  effective spec with one digest.
- Runtime machine:
  `absent -> starting -> warm -> busy -> draining -> absent`
  plus `failed`, `quarantined`, `missing`.
- Container launch only through the Docker Engine API from
  `services/spawn-controller/internal/backend` with hardening:
  pinned digest, `cap-drop ALL`, read-only rootfs, fixed uid, resource
  limits, tmpfs, network only along descriptor edges.
- The launch intent row lands in the registry before `docker create`.
- Warm runtime TTL: 15 minutes.
- Watchdog tick with a timestamp in the `fsm` schema; `/healthz` exposes
  the age of the last tick.

### task-engine (budget 4000 production lines)

- Job machine:
  `queued -> admitted -> running -> succeeded|failed`
  plus `uncertain`, `cancelled`, `quarantined`.
- Run machine: `dispatched -> streaming -> settled` with a deadline for
  stream start.
- Unique partial index "at most one non-final run per session".
- FIFO conversation ordering with a durable cursor.
- Admission ceilings.
- `uncertain` policy: 15 minutes of observation.
- Cancellation and holds.
- `hubctl job retry/cancel/unquarantine`.

### harness-runtime (budget 6000 production lines)

- Hermes container pinned by digest; all tools disabled.
- Config rendered host-side and mounted read-only; env is an exact list.
- NO DATABASE: run admission table lives only in memory; a row is created
  only by `SubmitRun` from task-engine; a run-id without a row is a
  refusal; caller headers are stripped.

### identity

- Fake that passes the conformance suite of its own contract; issues run
  and service tokens. The real identity-hub is phase 4.

### Scenario model

- Test container with an OpenAI-compatible API and one built-in scenario
  "answer with text", connected through the `models` passport route.
  `model-gateway` is phase 6.
- Tests submit jobs through `CreateJob`. There is no channel in this
  phase.

## Mandatory

- State lives only in `fsm.machines`; no `state`/`status` columns in
  domain tables; a side effect is an outbox row in the same transaction.
- A call between services carries a service token; the pair
  (sub, operation) is declared as an edge in the descriptor; a new
  operation starts with an edge.
- The `space` zone has no route to Postgres and no connection variables.
- The Docker client exists only inside the spawn-controller backend.
- Every service: its own binary, its own database and role, its own
  `contracts/<svc>/v1` with a conformance suite; imports limited to
  `shared/*`, `contracts/*` and its own `internal`.
- Service ADRs are drafts in this phase's PR; the owner accepts.

## Readiness (all in CI)

- e2e "task -> answer" green for three users in parallel; one user's data
  is not visible to another (`sys.tenant-isolation`).
- Killing the runtime container mid-run moves the job to `uncertain`
  with observation, not a retry (`sys.kill-uncertain`).
- Restarting the whole stack restores state from the databases without
  loss (`sys.stack-restart`).
- `sys.space-no-db`, `sys.worker-intent`, `sys.service-edge` green.
- A forged token is refused (`sys.forged-token`).
- A missing database refuses startup (`sys.store-missing`).
- `just check` green, fits in 5 minutes locally; every new gate is proven
  by a broken example.

## Out of scope

channel-hub and any channel, the real identity-hub, tool-hub and
capabilities, full-form passports and layers, credential-broker, egress
and model-gateway, memory, proactivity, workers and MCP.

## Process

- Atomic Conventional Commits in English, no AI attribution, no push.
- Do not weaken thresholds: a blocking check is a finding for the report.
- At the end: tag `v0.0.3`, merge into `main`, and
  `docs/phases/03-report.md` — what was done, measurements of the
  redesign signals from blueprint §9, what did not fit, questions for
  the owner.
