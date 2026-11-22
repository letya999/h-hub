# harness-runtime

## Purpose

Runs one admitted Hermes turn in a hardened container: the run exists
only because task-engine's `SubmitRun` planted an admit row in memory,
and the turn is one scripted "answer with text" model call.

## Zone and neighbors

Space zone. Called only by task-engine (`SubmitRun` with a run token,
`CancelRun`); calls back `ReportRun` with a service token. Reaches the
scripted model over the models-passport route; cannot reach Postgres or
the Docker socket (`sys.space-no-db`).

## Owns

The in-memory admit table (run-id -> owner/ctx), the mounted read-only
host-rendered config, and the turn runner — nothing is persisted.

## Contracts

Implements `contracts/harness/v1` (`SubmitRun`, `CancelRun`); consumes
`task/v1` (`ReportRun`). Fake at `contracts/harness/v1/fake.go`.

## Does not do

No database — a run-id without an admit row is a refusal, and caller
headers are stripped. No tools: all Hermes toolsets are disabled. No
restart of runs, no retries — settlement is reported once to
task-engine. No secrets of its own; `HHUB_CONFIG` points at the
rendered file.

## Machines

None of its own; run lifecycle lives in task-engine's Run machine and
the container lifecycle in spawn-controller's Runtime machine.

## Config

`HHUB_SUB`, `HHUB_CONFIG`; loaded once in `internal/config`. The env
allowlist is exact — anything else is absent in the container.

## Runbook

Runs as `hhub-rt-*` containers launched by spawn-controller. Healthy =
config file present and admit table empty or actively streaming. If a
run never settles, check `ReportRun` connectivity to task-engine and
the scripted-model route; a run that dies mid-turn surfaces as
`job.uncertain`, which is observed, never retried.
