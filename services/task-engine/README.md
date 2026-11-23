# task-engine

## Purpose

Owns the job lifecycle: it accepts tasks over `CreateJob`, orders work
per conversation (FIFO with a durable cursor), enforces admission
ceilings, and drives each admitted job through runtime dispatch to a
settled run.

## Zone and neighbors

Core zone. Called by `hubctl job` with owner-scoped operator tokens and by
harness-runtime (`ReportRun`). Calls identity-hub (`MintRunToken`,
`IssueServiceToken`), spawn-controller (`ResolveSpec`, `EnsureRuntime`,
`ReleaseRuntime`) and harness-runtime (`SubmitRun`, `CancelRun`).
Owner scope is verified before reads and writes; space ownership is
checked through `ResolveSpec` ([TestJobToAnswer](../../test/e2e/e2e_test.go)).

## Owns

Job and Run state machines (`fsm.machines`), `jobs`/`runs` domain
tables and the `job_events` history in its own database, the
conversation FIFO cursor, hold table, and the dispatch/outbox/sweep
loops. The single partial index "at most one non-final run per
session" lives here.

## Contracts

Implements `contracts/task/v1` (job ops and `ReportRun`);
consumes `identity/v1`, `spawn/v1`, `harness/v1`. Fake at
`contracts/task/v1/fake.go`.

## Does not do

No channel, no user credential store, no container
management (spawn-controller's line), no model calls (harness
runtime's line), no rights/grants.

## Machines

Job: `queued -> admitted -> running -> succeeded|failed`, `uncertain`
(loss observation, 15 min, never auto-retried), `cancelled`,
`quarantined`; `failed`/`quarantined` are `LongLived` with operator
exits (`retry`, `unquarantine`). Run: `dispatched -> streaming ->
settled` with start-of-stream deadline. Deadlines are swept by the
service's own sweeper.

## Config

`HHUB_SUB`, `HHUB_DSN`, `HHUB_KEYS`, `HHUB_IDENTITY_URL`,
`HHUB_SPAWN_URL`, `HHUB_CHANNEL_URL`; loaded once in `internal/config`.

## Runbook

Starts with `just stack`. Healthy = `/healthz` plus a moving watchdog
tick. If jobs sit `admitted`, check `EnsureRuntime` reachability; if
they fall to `uncertain`, inspect `job_events` for the lost run and
the runtime container's liveness. `hubctl job retry|cancel|
unquarantine` operates the `LongLived` exits.
The exit command for every job state is checked at the contract boundary
([TestJobExitCommandsCoverEveryNonfinalState](internal/svc/wedge_db_test.go)).
If a deadline is over 30 seconds late, the sweeper records an operator
alert and delivers it through channel-hub
([TestSweepLagOperatorAlert](../../test/e2e/sweep_lag_test.go)).
